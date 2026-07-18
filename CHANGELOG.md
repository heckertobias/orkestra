# Changelog

All notable changes to orkestra are documented here.

## [v0.1.0] — 2026-07-18

First published release. orkestra is a lightweight Master/Agent orchestrator for Docker Compose
stacks across Linux servers. Beyond the initial M0–M7 scaffold (detailed under *Foundation* below),
this release adds:

- **M8 — RBAC depth:** per-server / per-stack role bindings, a dedicated `secrets-manager` role,
  and a permissions-matrix UI.
- **Auth / SSO / self-service wave:** per-user SSO-only flag, RP-initiated (real) SSO logout with a
  choice page, last-local-admin invariant guards, admin set-password links, email (SMTP) flows via
  Mailpit in the dev harness.
- **Stack editor:** full-featured compose editor with whitelist-based field validation and line
  numbers, compose-aware highlighting, and declarative env vars resolved at deploy time.
- **Agent:** image pull in the converge engine honouring the compose `pull_policy`; end-to-end
  enrollment; isolated Docker-in-Docker agents in the dev harness.
- **M7 hardening (second pass):** agent gRPC port `4440`, TrueNAS container agent (custom app +
  catalog train), federated agent metrics through the Master.
- **Updates (foundation only):** `update_policies` / `available_updates` schema + queries, the
  `StatusReport.available_updates` wire format, and master-side persistence. Agent detection, apply
  RPCs, browser API and UI are **not** implemented yet — see
  [ROADMAP.md](ROADMAP.md#1-update-system-fleet-updates).
- **Dev harness:** `run-dev.sh` (Postgres + Master + Vite, auto admin/token, optional DinD agents)
  and a `run-dev` skill.
- **apt/dnf packages:** `.deb` + `.rpm` for `orkestra-master` and `orkestra-agent` (amd64/arm64)
  via goreleaser/nfpm, published to a GPG-signed GitHub Pages repository. Install and update with
  `apt install`/`apt upgrade` or `dnf install`/`dnf upgrade`; `install-agent.sh` stays as a fallback.
- **Web:** the "Add Server" button now opens an enrollment dialog (mint token + copy the
  `orkestra-agent enroll` command) — a browser-only agent connect.

### Foundation (M0–M7)

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
- Log/stats streaming wire format + Master↔Agent bridge scaffolding (not yet wired end-to-end —
  see [ROADMAP.md](ROADMAP.md#3-live-streaming--logs-stats-exec))
- React/TypeScript/Vite SPA, dark theme with lime-green accent, Server List and Server Detail pages

**M3 — Compose Stacks & Desired-State Reconciliation**
- Full stack CRUD with versioned `stack_versions` and `assignments`
- Compose Converge Engine: spec-hash–based container identity, create/recreate/remove for a Compose
  subset (image, command, env, ports, restart, bind mounts, …). Named networks/volumes, `depends_on`
  ordering and healthchecks are not yet applied — see
  [ROADMAP.md](ROADMAP.md#2-converge-engine--compose-coverage)
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
