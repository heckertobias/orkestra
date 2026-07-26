# orkestra — Security & Authentication

## Threat Model (Summary)

| Threat | Mitigation |
|---|---|
| Rogue Agent connects | mTLS: only certs signed by internal CA are accepted |
| Bootstrap token leaked | Tokens are short-lived, single-use (configurable), and revocable |
| Man-in-the-middle on Agent↔Master channel | mTLS mutual authentication + full TLS encryption |
| Credential stuffing on UI | argon2id hashing, configurable password policy, session revocation |
| Brute force on bootstrap endpoints | Per-IP rate limiting on `Enroll` and the first-run setup endpoint |
| Privilege escalation in UI | RBAC with scoped roles, enforced in the service layer behind the session interceptor |
| Secret exfiltration | Secrets never stored plaintext; never appear in list/get responses; reveal requires re-auth and is audited |
| DB / backup theft | CA private key and secret values are KEK-encrypted; KEK is held in a **separate trust domain** (file/secret-mount, never in the same config as DB credentials) |

---

## 1. Agent Identity — mTLS with Bootstrap Token

### Internal CA

On first start, the Master generates a **self-signed CA** (ECDSA P-384):
- CA cert is stored in `ca.cert_pem`.
- CA private key is encrypted with the KEK (loaded via `KeySource`, see below) and stored in
  `ca.key_enc`. The raw key is never written to disk or the DB.
- The CA cert is distributed to Agents as part of the `EnrollResponse.ca_bundle_pem`.
  Agents pin this cert for all subsequent TLS connections.

### Bootstrap Enrollment Flow

```
1. Operator creates a token in the UI:
   - TTL (e.g. 1 hour)
   - Max uses (e.g. 1 for a single agent, N for a batch)
   - Optional description
   Master generates a random 256-bit token, stores SHA-256(token) in enrollment_tokens.

2. Token is shown to the operator **once** (the raw token is never persisted).

3. On the target server:
   ./orkestra-agent enroll \
     --master https://master.example.com:4440 \
     --bootstrap-token <token> \
     --name "web-server-01"

4. Agent generates ECDSA P-384 keypair locally. Private key never leaves the server.

5. Agent sends: EnrollRequest{bootstrap_token, csr_pem, node_info}
   (over TLS with server-auth only — no client cert yet)

6. Master:
   a. Validates token (hash match, not expired, not over max_uses, not revoked).
   b. Increments used_count.
   c. Assigns the agent ID and signs the CSR with that ID as the certificate CN
      → 1-year client certificate.
   d. Inserts server record, inserts certificate record.
   e. Returns: EnrollResponse{agent_id, client_cert_pem, ca_bundle_pem}

7. Agent persists (in ORKESTRA_AGENT_DATA, default /etc/orkestra/agent):
   agent.crt    (client cert)
   agent.key    (private key, chmod 600)
   ca.crt       (CA bundle for server verification)
   config.json  (master address, agent_id)
```

The **Master assigns the identity**: the agent ID is generated server-side and used both as the
`servers.id` and as the certificate CN, so an agent cannot claim another identity through its CSR
subject. `Enroll` is the only unauthenticated AgentService RPC and is rate-limited per client IP.

### Ongoing mTLS

All subsequent connections use **mutual TLS**:
- The Agent presents its client cert (signed by the internal CA); the CN is the agent ID, and every
  request is attributed to the server row with that ID.
- The Master verifies: signed by the CA, not revoked. The server cert on `:4440` is issued by the
  **internal CA** — its SANs always include the loopback names plus everything listed in
  `ORKESTRA_AGENT_TLS_SANS`.
- The Agent verifies the Master against the pinned CA bundle it received at enrollment. This is why
  `:4440` must never be terminated by a public-certificate reverse proxy.

Revocation check: the Master keeps the revocation state in `certificates.revoked` and checks it on
every connection (no CRL/OCSP polling). A revoked agent cannot establish a new stream; an existing
stream is not actively torn down.

### Certificate Rotation

- Agent certs are valid for 1 year.
- Before each connection attempt the Agent checks its cert and calls `AgentService.RenewCert(csr)`
  when less than 30 days of validity remain.
- `RenewCert` requires the current (not-yet-expired) client cert for authentication and re-signs
  with the same CN, so the agent keeps its identity.
- Each issuance appends a row to `certificates`; the previous cert stays valid until it expires.

---

## 2. User Authentication

### Local Users (Default)

- Passwords are hashed with **argon2id** (memory 64 MB, time 1, parallelism 4, 16-byte salt,
  32-byte key — the OWASP minimum), stored in PHC format so the parameters travel with the hash and
  can be raised later without invalidating existing hashes.
- **First-run setup:** if no users exist, the Master generates a one-time setup token and logs the
  setup URL (`<public URL>/login?setup=<token>`). Opening it creates the first admin. The token
  lives in memory only, is invalidated the moment the admin is created, and a new one is generated
  on the next start as long as no user exists. `/api/setup` is rate-limited per IP.
- **Invite instead of assigned passwords:** creating a user does not set a password. The user gets
  an invite link (valid 72 h) and chooses their own password, which is checked against the password
  policy. The same mechanism backs admin-triggered "send password link" and self-service password
  reset (valid 1 h). All of these require SMTP to be configured.
- **Password policy:** min length and min/max counts for special characters, digits, upper and
  lower case, configurable in the UI (Settings → Password policy) and enforced on every path that
  sets a password. `0` means "no limit".
- Sessions use a **random session token** delivered in an `HttpOnly; SameSite=Lax` cookie. The
  `Secure` attribute is on by default (`ORKESTRA_SECURE_COOKIES`; disable only for plain-HTTP local
  dev) — the same applies to the transient OIDC `state` cookie. Only the SHA-256 of the token is
  stored in `sessions`; the raw token is never persisted.
- Session lifetime: 24 hours. Logout revokes the session row and clears the cookie.

### OIDC (Optional)

OIDC is configured **at runtime in the UI** (*Settings → OIDC*, admin only) and stored in the
`oidc_config` table — there is no config file. Saving the form reloads the provider immediately, so
no Master restart is needed.

| Field | Meaning |
|---|---|
| `enabled` | Turns the "Login with SSO" button and the `/auth/oidc/*` routes on |
| `issuer_url` | e.g. `https://sso.example.com/realms/myrealm` (discovery document is fetched from it) |
| `client_id` / `client_secret` | The secret is write-only in the API and KEK-encrypted at rest |
| `scopes` | Default `openid`, `profile`, `email`; add `groups` if you map roles |
| `groups_claim` | Which token claim holds group membership (default `groups`) |
| `claim_mapping` | Group value → orkestra role, e.g. `orkestra-admins → admin` |

A local Keycloak realm for testing lives in `dev/keycloak/`.

The redirect URI registered at the IdP must be `<public URL>/auth/oidc/callback`. Behind TLS the
Master must know its `https://` public origin so the `redirect_uri` matches the registration —
otherwise it falls back to the bind address over `http://` and the IdP rejects the mismatch. Set
the public URL either way:

- **In the UI** under *Settings → General → Public URL* (admin, stored in `server_config`). This is
  applied live: changing it re-initialises the OIDC provider so the new `redirect_uri` takes effect
  without a Master restart.
- **Via `ORKESTRA_PUBLIC_URL`** (e.g. `https://orkestra.example.com`) as the startup default when no
  UI value is set — useful for declarative/GitOps deployments.

The UI value takes precedence over the env var. When neither is set, the scheme defaults to `https`
if `ORKESTRA_SECURE_COOKIES` is enabled (the default). See `docs/08-deployment.md` § "Behind a
domain / reverse proxy".

OIDC login flow:
1. Browser clicks "Login with SSO" → Master redirects to OIDC provider.
2. Provider authenticates user, redirects back with `code`.
3. Master exchanges `code` for tokens, verifies ID token.
4. Master matches the identity to a **pre-existing** local user — first by `oidc_subject`,
   then by email (`username`), linking the `sub` on first login. There is **no** just-in-time
   provisioning: an unknown identity is redirected to `/login?error=oidc_no_account`. The user
   must be created in orkestra beforehand.
5. Maps groups/claims to roles per `claim_mapping`.
6. Creates session → sets cookie.

Both auth methods can coexist by default: a user can have both a local password and an OIDC
subject.

**SSO-only users.** A user can be flagged `sso_only` (at creation or via the user editor). Such a
user authenticates exclusively via OIDC: no invite email is sent, and every path that would set or
use a local password — `Login`, the invite/reset `set-password` flow, admin `ResetPassword`,
`SendPasswordLink`, and self-service `RequestPasswordReset` — is rejected server-side. Toggling
the flag off is lossless: any existing `password_hash` is left dormant (never cleared), so local
login is restored immediately.

**Local-admin invariant.** Flagging a user `sso_only` is blocked when it would remove the last
*local* admin — an enabled global admin who is not `sso_only` and has a password set (i.e. can log
in without the IdP). Enforced under the same advisory lock as the last-admin disable/revoke guards
(`withLastLocalAdminGuard`), so at least one admin can always log in even if the IdP is down.

**Logout.** `Logout` revokes the orkestra session and clears the cookie. For SSO sessions the
Master also stores the `id_token` (`sessions.oidc_id_token`) and returns an RP-initiated logout URL
(the IdP's `end_session_endpoint` with `id_token_hint` + `post_logout_redirect_uri`); the SPA then
offers the user a choice to *also* end the IdP session. Without this, the IdP session persists and a
subsequent SSO login re-authenticates silently (standard OIDC). Admins must register their
post-logout redirect URIs at the IdP (e.g. Keycloak client attribute `post.logout.redirect.uris`),
otherwise the IdP ends the session but will not redirect the browser back.

### API Keys

Non-browser clients authenticate with an **API key** instead of a session cookie: a `Bearer` token
that is shown once at creation and stored SHA-256-hashed in `api_keys`. A key inherits the role
bindings of the user it belongs to, can carry an expiry, and can be revoked. Users manage their own
keys under *Settings → API keys*; the typical consumer is a Prometheus scrape job hitting
`/api/agents/{id}/metrics`.

---

## 3. RBAC

### Roles

| Role | Permissions |
|---|---|
| `admin` | Full access: users, roles, servers, stacks, secrets, enrollment tokens, OIDC/SMTP/server config, audit log |
| `operator` | Create and edit stacks, assign/unassign/roll back, control containers, view logs and stats |
| `viewer` | Read-only: servers, stacks, containers, logs, stats. Never secret values. |
| `secrets-manager` | Create, update, delete, and reveal secrets. Grants no access to servers or stacks. |

### Role Bindings

A binding is `(user, role, server?, stack?)`. An empty scope means "any":

- **Global:** no server and no stack → the role applies everywhere.
- **Server-scoped:** the role applies to that server and the stacks deployed on it.
- **Stack-scoped:** the role applies to that stack (optionally only on one server).

For a given `(server, stack)` pair, the **highest matching role wins** (`viewer` < `operator` <
`admin`). `secrets-manager` sits outside that ladder: it is checked separately and deliberately
does not grant server or stack visibility.

Stack visibility is derived from assignments: an unassigned stack is visible to any operator (so it
can be assigned in the first place), while an assigned stack requires access on at least one of the
servers it runs on. Deleting a stack requires operator access on **all** servers it is assigned to.

### Enforcement

Enforcement happens in two layers:

1. A **session interceptor** resolves the session cookie or `Bearer` API key into a user context
   (including all role bindings) and rejects unauthenticated calls to non-public procedures.
2. Every service method then asks the RBAC helpers in `internal/master/auth` (`IsAdmin`,
   `CanViewOn`, `CanOperateOn`, `CanViewStack`, `CanManageSecrets`, …) and returns
   `connect.CodePermissionDenied` when the answer is no. List endpoints filter rather than fail.

Raw database access (the store package) does **not** enforce RBAC — that is the service layer's
job, and it is the single enforcement point. The SPA mirrors the same rules for gating UI elements
(`web/src/lib/rbacMatrix.ts`), but that is cosmetic: the server decides.

---

## 4. Transport Security

| Endpoint | TLS | Auth |
|---|---|---|
| `:4440` (Agent gRPC) | mTLS, terminated by the Master (required) | Client cert signed by the internal CA |
| `:8080` (UI + API) | Plain HTTP — terminate TLS at a reverse proxy | Session cookie or `Bearer` API key |
| `:9090` (Prometheus metrics, Master) | None | None — bind to loopback or firewall it |
| `:9091` (Prometheus metrics, Agent) | None | None — local only; federated through the Master |

The Master does **not** serve TLS on the UI port itself: put nginx / Caddy / Traefik in front of
`:8080` and terminate a real certificate (e.g. Let's Encrypt) there. `:4440` is the opposite case —
it must reach the Master undecrypted, because agents pin the internal CA (see
[08-deployment.md](08-deployment.md) § "Ingress & Networking").

---

## 5. Audit Log

Mutating actions write an `audit_log` entry through the service layer:

```go
store.InsertAuditLogParams{
    Ts:         …,          // Unix ms
    ActorID:    …,          // acting user
    ActorName:  …,
    Action:     "secret.reveal",   // dotted verb
    TargetType: "secret",          // "user" | "secret" | ...
    TargetID:   …,
    BeforeJson: …, AfterJson: …,   // optional snapshots
    IPAddress:  …,
    Error:      …,          // set when the action failed
}
```

Covered today:

| Area | Actions |
|---|---|
| Authentication | `auth.login`, `auth.logout`, `auth.change_password` |
| Users | `user.create`, `user.delete`, `user.update_profile`, `user.email_changed`, `user.send_password_link` |
| Secrets | `secret.create`, `secret.update`, `secret.delete`, `secret.reveal` |

Stack, assignment, server, and role-binding mutations are not audited yet — that gap is worth
keeping in mind when using the audit log for change tracking.

Secret values never appear in audit entries. The log is append-only from the application's
perspective: there is no delete or update path in the store, only `InsertAuditLog`. The UI provides
a searchable view for admins.

---

## 6. KEK & KeySource

### Why the KEK Must Be in a Separate Trust Domain

The KEK (Key-Encrypting Key) protects four things *at rest* in the database: the CA private key,
built-in secret ciphertexts, the OIDC client secret, and the SMTP password. Its purpose is to make
a DB dump or backup useless on its own — an attacker with only the database still cannot read the
encrypted material.

**This protection is void if the KEK lives alongside the DB credentials** (e.g. same `.env` file
or Compose `environment:` block). Whoever has the config has both. The KEK only provides real
defense when held in a **separate trust domain**.

### KeySource Abstraction (`internal/master/keys/`)

The Master resolves the KEK at startup via a pluggable `KeySource` interface:

```go
type KeySource interface {
    Load(ctx context.Context) ([]byte, error)  // returns the 32-byte KEK
}
```

Auto-selection priority: `ORKESTRA_MASTER_KEY_FILE` set → **file** source; else
`ORKESTRA_MASTER_KEY` set → **env** source (with a startup warning); else startup error.

| Source | Env var / config | Notes |
|---|---|---|
| **file** *(recommended)* | `ORKESTRA_MASTER_KEY_FILE=/run/secrets/orkestra_master_key` | Docker/K8s `secrets:` mount (tmpfs) or a root-only `chmod 600` file. Value never appears in config. Allows unattended restart. |
| **env** *(dev/test only)* | `ORKESTRA_MASTER_KEY=<hex>` | Logs a warning on startup. Acceptable for local dev; not for production. |

The interface exists so further sources (an interactive "sealed start", or a KMS that unwraps the
KEK at boot) can be added without touching call sites. Only `file` and `env` are implemented.

### Deployment Rule

The KEK must **never** appear in the same file or secret store as the database credentials. Store
it as a Docker/K8s `secret:` (mounted as tmpfs), a systemd `LoadCredential`, or a dedicated
`chmod 600` file owned by `root` — completely separate from `.env` and Compose `environment:`.

---

## 7. Security Checklist for Deployment

- [x] KEK is provided via `ORKESTRA_MASTER_KEY_FILE` pointing to a Docker/K8s secret mount or a
      `chmod 600` file — **never** as a plain env var in the same config as DB credentials.
- [x] KEK is a random 256-bit value, backed up separately from the database (password manager / HSM).
- [x] PostgreSQL access is restricted to the `orkestra` DB user; TLS is enforced on the connection.
- [x] Port `:4440` is firewalled to Agent IPs only (or the Master is on a private network).
- [x] Port `:9090` (and the agents' `:9091`) is bound to loopback or protected by a scrape-IP
      allowlist — the metrics endpoints are unauthenticated.
- [x] A reverse proxy terminates a valid TLS certificate in front of `:8080`, and
      `ORKESTRA_SECURE_COOKIES` is left at its default (`true`).
- [x] Bootstrap tokens are single-use and have short TTLs (< 1 hour).
- [x] Agent hosts' `/var/run/docker.sock` is accessible only to the `orkestra-agent` user.
- [x] Regular backups of the PostgreSQL database (`pg_dump`) and the KEK stored **separately**.

### PostgreSQL Backup

```bash
# Dump the database (run on the host or inside the postgres container):
pg_dump -U orkestra -h localhost orkestra | gzip > orkestra_$(date +%Y%m%d).sql.gz

# Restore:
gunzip -c orkestra_20260607.sql.gz | psql -U orkestra -h localhost orkestra
```

Store the dump **separately** from the KEK. A dump without the KEK cannot decrypt secrets or the CA private key. A KEK without the dump is useless. Both are needed together to recover a cluster.
