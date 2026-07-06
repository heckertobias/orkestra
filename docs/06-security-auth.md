# orkestra — Security & Authentication

## Threat Model (Summary)

| Threat | Mitigation |
|---|---|
| Rogue Agent connects | mTLS: only certs signed by internal CA are accepted |
| Bootstrap token leaked | Tokens are short-lived, single-use (configurable), and revocable |
| Man-in-the-middle on Agent↔Master channel | mTLS mutual authentication + full TLS encryption |
| Credential stuffing on UI | argon2id hashing, session invalidation, rate limiting |
| Privilege escalation in UI | RBAC with scoped roles, enforced at Connect middleware layer |
| Secret exfiltration | Secrets never stored plaintext; never appear in API responses |
| Audit bypass | All mutations go through audited service methods (not raw DB access) |
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
     --master https://master.example.com:8443 \
     --bootstrap-token <token> \
     --name "web-server-01"

4. Agent generates ECDSA P-384 keypair locally. Private key never leaves the server.

5. Agent sends: EnrollRequest{bootstrap_token, csr_pem, node_info}
   (over TLS with server-auth only — no client cert yet)

6. Master:
   a. Validates token (hash match, not expired, not over max_uses, not revoked).
   b. Increments used_count.
   c. Signs the CSR → 1-year client certificate (configurable).
   d. Inserts server record, inserts certificate record.
   e. Returns: EnrollResponse{agent_id, client_cert_pem, ca_bundle_pem}

7. Agent persists:
   /etc/orkestra/agent/agent.crt  (client cert)
   /etc/orkestra/agent/agent.key  (private key, chmod 600)
   /etc/orkestra/agent/ca.crt     (CA bundle for server verification)
   /etc/orkestra/agent/config.yaml (master address, agent_id)
```

### Ongoing mTLS

All subsequent connections use **mutual TLS**:
- Agent presents its client cert (signed by internal CA).
- Master verifies: cert is signed by CA, CN matches `agent_id`, cert is not revoked.
- Master presents its server cert (can be a Let's Encrypt cert or the internal CA).
- Agent verifies: server cert is valid and matches the pinned CA bundle.

Revocation check: the Master maintains a revocation list in `certificates.revoked`. The check
happens at connection time (not CRL/OCSP polling). Revoked agents are immediately disconnected.

### Certificate Rotation

- Agent certs are valid for 1 year by default (configurable: `pki.cert_ttl`).
- The Agent monitors cert expiry and calls `AgentService.RenewCert(csr)` when 30 days remain.
- `RenewCert` requires the current (not-yet-expired) client cert for authentication.
- The old cert is revoked after the new one is confirmed active.

---

## 2. User Authentication

### Local Users (Default)

- Passwords hashed with **argon2id** (params: memory=64MB, iterations=3, parallelism=4).
- First-run setup: if no users exist, Master prints a one-time setup URL to stdout.
  Operator opens URL, sets admin username + password. URL expires after 30 minutes.
- Sessions use a **random 256-bit session token** stored in an `httponly; Secure; SameSite=Strict`
  cookie. Token is stored as SHA-256 in the `sessions` table (raw token never persisted).
- Session TTL: 8 hours idle, 7 days absolute (configurable).

### OIDC (Optional)

Configuration stored in `oidc_config` (encrypted client secret):

```yaml
auth:
  oidc:
    enabled: true
    issuer_url: "https://sso.example.com/realms/myrealm"
    client_id: "orkestra"
    client_secret: "${BAO_SECRET}"  # or plain value
    scopes: ["openid", "profile", "email", "groups"]
    claim_mapping:
      groups:
        "orkestra-admins": "admin"
        "orkestra-ops":    "operator"
        "orkestra-view":   "viewer"
```

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
login is restored immediately. Note: there is no guard against flagging the last remaining admin
as `sso_only`; if the IdP is unavailable there would then be no local admin fallback.

### CSRF Protection

All mutating API requests (non-GET Connect RPCs) require a `X-CSRF-Token` header containing a
CSRF token derived from the session. The browser SPA includes it automatically. Pure API clients
(non-browser) use API keys instead of session cookies (to be implemented in M5).

---

## 3. RBAC

### Roles

| Role | Permissions |
|---|---|
| `admin` | Full access: manage users, roles, servers, stacks, secrets, tokens, OIDC config |
| `operator` | Deploy, start, stop, restart, pull, view logs/stats; create stacks; manage own secrets |
| `viewer` | Read-only: view servers, stacks, containers, logs, stats. Cannot see secret values. |

### Role Bindings

Bindings can be:
- **Global:** user has the role for all resources.
- **Server-scoped:** user has the role only for a specific server (and its stacks).
- **Stack-scoped:** user has the role only for a specific stack (across all servers it's deployed to).

### Enforcement

RBAC is enforced as a **Connect interceptor** (`internal/master/auth/rbac_interceptor.go`) that:
1. Extracts the authenticated user from the request context (set by the session interceptor).
2. Looks up the user's effective roles for the target resource.
3. Returns `connect.CodePermissionDenied` if insufficient.

Raw database access (repositories) does **not** enforce RBAC — it is the service layer's
responsibility. This is intentional: the interceptor is the single enforcement point.

---

## 4. Transport Security

| Endpoint | TLS | Auth |
|---|---|---|
| `:8443` (Agent gRPC) | mTLS (required) | Client cert (signed by internal CA) |
| `:8080` (UI + API) | TLS (server-only, or reverse proxy) | Session cookie / API key |
| `:9090` (Prometheus metrics) | None (bind loopback by default) | — |

For `:8080`, it is recommended to terminate TLS at a reverse proxy (nginx/Caddy/Traefik) with
a proper certificate (Let's Encrypt). The Master can also serve TLS directly with a configured
cert.

---

## 5. Audit Log

Every mutating action writes an `audit_log` entry:

```go
type AuditEntry struct {
    Actor      string      // user ID or "system"
    ActorName  string
    Action     string      // "stack.deploy", "secret.update", "server.delete", ...
    TargetType string
    TargetID   string
    Before     interface{} // JSON-marshallable snapshot (sensitive fields redacted)
    After      interface{}
    IPAddress  string
    Error      string      // if the action failed
}
```

Secret values are **always redacted** in audit entries (replaced with `"[REDACTED]"`).

The audit log is append-only from the application's perspective; the DB user running the Master
has INSERT permission only on `audit_log` (enforced by not exposing a `DELETE` method in the
repository). Operators with DBA access can query it directly; the UI provides a searchable view.

---

## 6. KEK & KeySource

### Why the KEK Must Be in a Separate Trust Domain

The KEK (Key-Encrypting Key) protects three things *at rest* in the database: the CA private key,
builtin secret ciphertexts, and the OIDC client secret. Its purpose is to make a DB dump or backup
useless on its own — an attacker with only the database still cannot read the encrypted material.

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
| **env** *(dev/test only)* | `ORKESTRA_MASTER_KEY=<hex>` | Logs a warning on startup. Acceptable for local dev; not recommended in production. |
| **interactive** *(planned)* | — | Master starts "sealed"; operator enters key at runtime via TTY prompt or an unseal endpoint. Nothing persisted. Breaks auto-restart. |
| **kms** *(planned)* | `ORKESTRA_KEY_SOURCE=kms` | KEK is wrapped by an external KMS (OpenBao Transit or Cloud KMS); unwrapped at boot via API. No plaintext at rest, unattended restart works. |

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
- [x] Port `:8443` is firewalled to Agent IPs only (or the Master is on a private network).
- [x] Port `:9090` is bound to loopback or protected by a scrape-IP allowlist.
- [x] TLS cert on `:8080` is valid (Let's Encrypt or internal PKI).
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
