# orkestra — Secrets

## Design Goals

- **No plaintext at rest** — secret values are always encrypted before persisting.
- **No plaintext in transit (except over mTLS)** — secrets travel only over the mTLS-secured
  Agent↔Master stream, never in compose YAML or API responses.
- **No plaintext on Agent disk** — secrets are materialized into memory (tmpfs or Docker Secret
  API) and removed when the stack is stopped or removed.
- **Pluggable backends** — operators choose between a built-in encrypted store and OpenBao,
  without changing any application code.
- **Full audit** — every secret access (read, write, deploy-resolution) is logged to `audit_log`.

---

## `SecretProvider` Interface

```go
// internal/master/secrets/provider.go

package secrets

import "context"

type SecretRef struct {
    ID      string // orkestra secret UUID
    Name    string // human-readable name
    Version int    // version number (builtin only; ignored for openbao)
}

type SecretValue struct {
    Data    []byte // raw secret bytes (plaintext, in memory only)
    Version int    // resolved version
}

type SecretMeta struct {
    ID          string
    Name        string
    Provider    string // "builtin" | "openbao"
    Version     int
    Description string
    UpdatedAt   int64
}

type Capabilities struct {
    NativeVersioning bool // OpenBao has native KV versioning
    NativeRotation   bool
}

type Provider interface {
    // Get resolves a secret to its plaintext value (called at deploy time, never cached).
    Get(ctx context.Context, ref SecretRef) (SecretValue, error)

    // Set stores or updates a secret. Increments version.
    Set(ctx context.Context, ref SecretRef, val []byte) error

    // List returns metadata (never values) for secrets under a prefix.
    List(ctx context.Context, prefix string) ([]SecretMeta, error)

    // Delete removes a secret permanently.
    Delete(ctx context.Context, ref SecretRef) error

    // Capabilities returns what this backend supports natively.
    Capabilities() Capabilities
}
```

The active provider is selected at Master startup via config (`secrets.provider: builtin | openbao`)
and injected via dependency injection into every component that needs it.

---

## `builtin` Provider

### Encryption

- Key material: 32-byte KEK loaded via `KeySource` at startup (see `internal/master/keys/` and
  `docs/06-security-auth.md` § "KEK & KeySource"). Recommended: `ORKESTRA_MASTER_KEY_FILE`
  pointing to a Docker/K8s secret mount or a root-only file.
- Algorithm: **NaCl secretbox** (`golang.org/x/crypto/nacl/secretbox` — XSalsa20-Poly1305).
- Each value is encrypted as: `nonce (24 bytes) || ciphertext`, stored in `secrets.ciphertext`.
- The KEK is never stored in the database. Losing it makes all builtin secrets unrecoverable.

### Versioning

On `Set`, the existing row is updated with an incremented `version` and new `ciphertext`.
History is **not** kept in the builtin store (for history, use OpenBao's KV v2).

### Capabilities

```go
Capabilities{NativeVersioning: false, NativeRotation: false}
```

---

## `openbao` Provider

### Authentication

The Master authenticates to OpenBao using one of:
- **Token auth** (simplest, for development): `BAO_TOKEN` env var.
- **AppRole auth** (recommended for production): `BAO_ROLE_ID` + `BAO_SECRET_ID` env vars;
  the provider automatically renews the token before expiry.

Configuration:
```yaml
secrets:
  provider: openbao
  openbao:
    address: "https://bao.internal:8200"
    auth_method: approle   # or: token
    role_id: "..."          # if approle
    secret_id: "..."        # if approle
    kv_mount: "secret"      # KV v2 mount path
    namespace: ""           # OpenBao namespace (if used)
    ca_cert: "/etc/orkestra/bao-ca.pem"  # optional
```

### Secret Path

For a orkestra secret with `bao_mount=secret`, `bao_path=myapp/db-password`, `bao_key=value`:
- **Read:** `GET /v1/secret/data/myapp/db-password` → `.data.data.value`
- **Write:** `POST /v1/secret/data/myapp/db-password` `{"data": {"value": "<val>"}}`

The `bao_key` defaults to `"value"` if not set.

### Capabilities

```go
Capabilities{NativeVersioning: true, NativeRotation: true}
```

---

## Secret Distribution & Materialization

### At Deploy Time

1. Master receives a deploy request (or reconciler triggers on assignment change).
2. Master resolves all `secret_refs` from the `stack_version` record by calling
   `provider.Get(ctx, ref)` for each — **in the Master process, in memory only**.
3. Resolved values are added to the `ApplyDesiredState.stacks[].secrets` list as
   `ResolvedSecret{name, value (bytes), target}`.
4. The message is sent **only over the mTLS gRPC stream** to the Agent.
5. Master **does not log** secret values; only the audit event `secret.resolved` (name only) is
   written.

### On the Agent

The Agent receives `ResolvedSecret` values and materializes them depending on `target`:

#### `ENV` target
```go
// The secret value is injected into the container via ContainerCreate.Config.Env
// as "KEY=value". The value exists in the container's environment only.
// The Agent holds the plaintext in memory only during ContainerCreate, then drops it.
```

#### `FILE` target (tmpfs)
```go
// 1. Agent creates a tmpfs volume for the stack (if not exists):
//    docker volume create --driver local --opt type=tmpfs --opt device=tmpfs
//      orkestra_<stack_id>_secrets
// 2. Before ContainerCreate, the Agent starts a short-lived helper container that
//    writes the secret file into the tmpfs volume.
// 3. The file is mounted into the service container at file_path (read-only).
// 4. On STOPPED/REMOVED, the tmpfs volume (and its contents) is removed.
```

#### `DOCKER_SECRET` target
```go
// 1. Agent calls docker.SecretCreate({Name: "orkestra_<stack_id>_<name>", Data: value})
// 2. Service container config references the Docker Secret by name.
// 3. On REMOVED, Agent calls docker.SecretRemove.
// Note: Docker Secrets require Swarm mode. In non-Swarm setups, this target
// falls back to FILE/tmpfs automatically, with a warning in the StatusReport.
```

### On Stack Stop / Remove

- `ENV` secrets: gone when the container stops (they lived only in process memory).
- `FILE` secrets: the tmpfs volume is removed → data never hits disk.
- `DOCKER_SECRET` secrets: Docker secret is removed.

---

## Secret Bindings in stack_versions

The `secret_refs` column in `stack_versions` is a JSON array:

```json
[
  {
    "name": "db_password",
    "secret_id": "uuid-of-secret",
    "service_name": "web",
    "target": "env",
    "env_key": "DB_PASSWORD"
  },
  {
    "name": "tls_cert",
    "secret_id": "uuid-of-tls-secret",
    "service_name": "",
    "target": "file",
    "file_path": "/run/secrets/tls.crt"
  }
]
```

`service_name: ""` means the binding applies to all services in the stack.

---

## UI Behaviour

- Secret **values are never returned** by the API — only metadata (name, provider, version,
  description, binding info).
- The UI shows a masked placeholder (`••••••••`) and offers a "Reveal" action that requires
  re-authentication (confirming password or OIDC session) before the value is shown once.
  This reveal is also audit-logged.
- Creating/updating a secret opens a modal with a password-type input. The value is sent as
  a POST body over HTTPS and immediately encrypted by the Master before being stored/forwarded.

---

## Operational Notes

### Rotating a Secret

1. Update the secret value in the UI (or via OpenBao directly).
2. orkestra automatically triggers a reconcile for all stacks that reference the secret.
3. Affected containers are recreated with the new value.

### Backing Up builtin Secrets

Back up both:
- The PostgreSQL database (use `pg_dump`).
- The KEK (the file referenced by `ORKESTRA_MASTER_KEY_FILE`, or `ORKESTRA_MASTER_KEY` in dev).
  Store it **separately** from the DB backup — e.g. in a password manager or HSM.
  The two must never be co-located or the encryption provides no protection.

Without the KEK, the ciphertexts in the DB are unrecoverable.

### Migrating builtin → OpenBao

1. Configure OpenBao connection in Master config.
2. In the UI: for each secret, use "Migrate to OpenBao" action — Master reads plaintext via
   builtin, writes to OpenBao, updates `secrets.provider` + `bao_*` fields, clears `ciphertext`.
3. Restart Master with `secrets.provider: openbao`.
