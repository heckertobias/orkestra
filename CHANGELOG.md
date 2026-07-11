# Changelog

All notable changes to orkestra are documented here.

## [v0.1.0] — 2026-06-07

Initial release. orkestra is a lightweight Master/Agent orchestrator for Docker Compose stacks across Linux servers.

### Features

**M0 — Repo Scaffolding**
- Go module, buf/proto code generation, sqlc, Makefile, GitHub Actions CI
- PostgreSQL schema (full: PKI, auth, stacks, secrets, audit, events)
- Both binaries compile; `/healthz` endpoint

**M1 — PKI, Enrollment & mTLS**
- Internal CA (ECDSA P-384) with KEK-encrypted key storage
- Bootstrap token enrollment: agent generates keypair, CSR → master signs → 1-year client cert
- Persistent mTLS gRPC bidi-stream (Agent → Master) with exponential-backoff reconnect
- Heartbeat / offline detection (3 missed heartbeats → server marked offline)

**M2 — Container Control & Web UI**
- Agent-side Docker control: list, start, stop, restart, remove, image pull
- Log streaming and stats streaming bridged to browser
- React/TypeScript/Vite SPA, dark theme with lime-green accent, Server List and Server Detail pages

**M3 — Compose Stacks & Desired-State Reconciliation**
- Full stack CRUD with versioned `stack_versions` and `assignments`
- Compose Converge Engine: spec-hash–based container identity, create/recreate/remove, networks, volumes
- Master reconciler pushes `ApplyDesiredState` to connected agents every 15s (and on mutations)
- Stacks List and Stack Detail pages with version history and YAML viewer

**M4 — Secrets**
- Built-in secrets provider (XChaCha20-Poly1305 + KEK)
- Secret CRUD with masked values, reveal-with-reauth
- Audit logging for all secret operations

**M5 — User Auth, Sessions & RBAC**
- First-run setup flow (one-time URL in master logs)
- Local users: argon2id password hashing, httponly session cookie
- RBAC: `admin`, `operator`, `viewer` roles with Connect interceptor enforcement
- Login page, Users & Roles page, Audit Log page

**M6 — OIDC, Metrics, Event Feed & Polish**
- OIDC provider integration (`coreos/go-oidc`): claim→role mapping, encrypted client secret
- Prometheus metrics on `:9090` (Master) / `:9091` (Agent)
- `/healthz` + `/readyz` endpoints
- `events` table with live `StreamEvents` server-streaming feed
- API key auth (Bearer token, SHA-256 hashed) for non-browser clients
- Per-IP rate limiting on auth endpoints (`golang.org/x/time/rate`)
- Dashboard with fleet stats, loading skeletons, and live event feed
- Settings page: OIDC configuration and API key management

**M7 — Hardening, Packaging & Release**
- Agent cert auto-renewal: 30 days before expiry, new keypair + CSR → `RenewCert` RPC
- mTLS revocation check in `MTLSMiddleware`: DB fingerprint lookup, revoked agents get 403
- Systemd hardening for both units: `NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`, `PrivateDevices`, `ProtectKernelTunables`, `ProtectControlGroups`, `RestrictRealtime`, `LockPersonality`
- Docker Compose: master healthcheck (`/healthz`), metrics port `9090` exposed
- `Dockerfile.agent` for distroless agent image
- goreleaser configuration: binaries + `.tar.gz` archives + Docker images for amd64/arm64 with multi-platform manifests
- Security checklist reviewed (see `docs/06-security-auth.md`)

### Deployment

```bash
# Master (Docker Compose)
cd deploy/docker && docker compose up -d

# Agent (bare metal)
curl -fsSL https://github.com/heckertobias/orkestra/releases/latest/download/install-agent.sh | \
  bash -s -- --master https://master.example.com:4440 --bootstrap-token <token>
```

### Configuration

| Env var | Default | Description |
|---|---|---|
| `ORKESTRA_DATABASE_URL` | — | PostgreSQL DSN (required) |
| `ORKESTRA_MASTER_KEY_FILE` | — | Path to 32-byte hex KEK file |
| `ORKESTRA_AGENT_ADDR` | `0.0.0.0:4440` | Agent gRPC listener |
| `ORKESTRA_UI_ADDR` | `0.0.0.0:8080` | Web UI + API listener |
| `ORKESTRA_METRICS_ADDR` | `0.0.0.0:9090` | Prometheus metrics listener |
| `ORKESTRA_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |

[v0.1.0]: https://github.com/heckertobias/orkestra/releases/tag/v0.1.0
