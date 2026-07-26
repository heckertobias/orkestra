# orkestra — Secrets

> **Scope of this document.** It describes what the secrets subsystem does **today**: a built-in
> encrypted secret store with CRUD, reveal-with-reauth, and audit. Two larger pieces are designed
> but **not implemented yet** — delivering secrets into running deployments (materialization) and
> an external secrets backend.

## Design Goals

- **No plaintext at rest** — secret values are encrypted with the KEK before being persisted.
- **No plaintext in API responses** — list/get return metadata only; the value is returned solely
  by an explicit, re-authenticated `RevealSecret` call.
- **Full audit** — every create/update/delete/reveal is written to `audit_log`.
- **Room for a second backend** — a `provider` column already distinguishes `builtin` from
  `openbao`, but only the built-in provider is implemented.

---

## Built-in Provider (the only backend today)

### Storage & encryption

Secrets live in the `secrets` table (see `docs/03-data-model.md`). For `builtin` secrets the value
is encrypted and stored in the `ciphertext` column:

- **Algorithm:** XChaCha20-Poly1305 with the 32-byte KEK, via `pki.Encrypt`/`pki.Decrypt`
  (`internal/master/secrets/provider.go` exposes thin `Seal`/`Open` wrappers).
- The KEK is loaded at startup via the `KeySource` abstraction (see
  `docs/06-security-auth.md` § "KEK & KeySource"). It is **never** stored in the database — losing
  it makes all built-in secret ciphertexts unrecoverable.

### RPCs (`SecretService`)

Implemented in `internal/master/api/secrets.go`. All mutating calls require the
**secrets-manager** or **admin** role (`CanManageSecrets`):

| RPC | Behaviour |
|---|---|
| `ListSecrets` / `GetSecret` | Return metadata only (name, provider, version, description, binding count, `bao_*` fields). Never the value. |
| `CreateSecret` | `builtin` requires `value_bytes` → sealed with the KEK; starts at version 1. |
| `UpdateSecret` | Re-seals a new `value_bytes` (if provided) and/or updates the description. |
| `DeleteSecret` | Refused with `FailedPrecondition` while the secret still has active bindings. |
| `RevealSecret` | Requires a re-authentication password (verified against the caller's stored hash); `builtin` only; returns the plaintext once. Audit-logged. |
| `MigrateProvider` | Returns `CodeUnimplemented` — there is no second provider to migrate to. |

### UI behaviour

- Values are shown as a masked placeholder (`••••••••`). "Reveal" triggers `RevealSecret`, which
  re-authenticates before returning the plaintext once, and is audit-logged.
- Create/Update use a password-type input; the value is sent over HTTPS and sealed by the Master
  before it is stored.

---

## Secret Bindings (data model)

The `secret_refs` column on `stack_versions` is a JSON array describing which secret is injected
into which service and how:

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

> ⚠️ **Not wired into deployments yet.** The Master writes `secret_refs` as an empty array
> (`internal/master/api/stacks_crud.go`) and the reconciler does **not** resolve secrets into
> `ApplyDesiredState`, so bound secrets do not reach containers. The end-to-end path — resolution →
> `ResolvedSecret` over mTLS → agent-side ENV/FILE/DOCKER_SECRET materialization → cleanup on
> stop/remove — plus a bindings editor in the UI are still open.

---

## Operational Notes

### Backing up built-in secrets

Back up both, stored **separately**:

- The PostgreSQL database (`pg_dump`) — holds the ciphertexts.
- The KEK (the file referenced by `ORKESTRA_MASTER_KEY_FILE`) — in a password manager / HSM.

Without the KEK, the ciphertexts in the DB are unrecoverable. The two must never be co-located, or
the encryption provides no protection.

## Known limitations

- **Secrets are not delivered to deployments yet** — see the binding note above.
- **OpenBao is not implemented** — the `openbao` provider value and the `bao_*` columns exist as
  schema, but no code reads or writes OpenBao, and `RevealSecret` / `MigrateProvider` reject it.
- **No value history and no native rotation** in the built-in store: updating a secret bumps
  `version` and replaces the ciphertext; the previous value is gone.
