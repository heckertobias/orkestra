# orkestra — Repository Layout & Build Tooling

## Directory Structure

```
orkestra/
├── cmd/
│   ├── orkestra-master/        # Master entrypoint (flags only)
│   └── orkestra-agent/         # Agent entrypoint (subcommands: serve | enroll)
├── proto/
│   └── orkestra/v1/
│       ├── agent.proto          # Agent↔Master stream, Enroll, RenewCert
│       ├── stacks.proto         # Servers/Stacks/Assignments, compose validation, streams (UI)
│       ├── secrets.proto        # Secret CRUD API
│       ├── auth.proto           # Users, sessions, OIDC, SMTP, policies, tokens, API keys
│       └── common.proto         # Shared message types
├── internal/
│   ├── master/
│   │   ├── agentgw/             # Agent Gateway: Connect/gRPC handler, mTLS middleware, session registry
│   │   ├── api/                 # Connect handlers for the UI API (auth, stacks, secrets)
│   │   ├── auth/                # Session middleware, cookies, RBAC helpers, rate limiting
│   │   ├── oidc/                # OIDC provider: login/callback/status handlers, RP-initiated logout
│   │   ├── email/               # SMTP mailer (invite, password reset, email change)
│   │   ├── keys/                # KeySource abstraction — loads the KEK at startup (file/env)
│   │   ├── pki/                 # Internal CA, cert issuance, enrollment tokens, encrypt/decrypt
│   │   ├── secrets/             # Built-in secret sealing (KEK)
│   │   ├── reconciler/          # Desired-state builder → ApplyDesiredState to Agents
│   │   ├── metrics/             # Prometheus collectors (Master)
│   │   └── store/               # PostgreSQL layer
│   │       ├── migrations/      # goose migrations (applied at Master start)
│   │       ├── queries/         # sqlc source queries
│   │       └── *.sql.go         # sqlc-generated, type-safe query code (committed)
│   ├── agent/
│   │   ├── conn/                # gRPC client, mTLS, reconnect/backoff, heartbeat, cert renewal
│   │   ├── enroll/              # Bootstrap enrollment (token → cert) + on-disk config
│   │   ├── dockerctl/           # Docker Engine SDK wrapper
│   │   ├── compose/             # compose-go loader + Converge Engine
│   │   ├── reconcile/           # Local reconcile loop against desired state
│   │   ├── telemetry/           # Log / stats / Docker-event streamers
│   │   └── metrics/             # Prometheus collectors (Agent) + text-format gatherer
│   ├── shared/
│   │   ├── compose/             # Compose validation shared by Master (editor) and Agent
│   │   ├── gen/                 # Generated protobuf + Connect code, Go (gitignored)
│   │   └── version/             # Build-time version info
│   └── e2e/                     # End-to-end tests (Postgres, Docker daemon)
├── web/                         # React SPA (Vite + TypeScript)
│   ├── src/
│   │   ├── routes/              # TanStack Router file-based routes
│   │   ├── pages/               # Page components
│   │   ├── components/          # Layout, dialogs, UI primitives
│   │   └── lib/                 # API helpers, auth context, RBAC matrix
│   ├── gen/                     # Generated TypeScript Connect clients (gitignored)
│   ├── dist/                    # Vite build output, embedded into the Master binary
│   ├── ui_prod.go               # go:embed of web/dist + SPA fallback handler
│   └── ui_dev.go                # `dev` build tag: reverse proxy to the Vite dev server
├── deploy/
│   ├── systemd/                 # orkestra-master.service, orkestra-agent.service
│   ├── docker/                  # Dockerfiles + Compose setup for self-hosting the Master
│   ├── packaging/               # deb/rpm scaffolding: env files, pre/post-install scripts
│   ├── truenas/                 # TrueNAS SCALE custom-app YAML + catalog app for the Agent
│   └── install-agent.sh         # Fallback installer (download binary, enroll, install service)
├── dev/
│   └── keycloak/                # Keycloak realm import for local OIDC testing
├── docs/                        # Design documentation (this directory)
├── buf.yaml / buf.gen.yaml      # protobuf module + codegen config
├── sqlc.yaml                    # sqlc config
├── Makefile                     # Developer shortcuts
├── .goreleaser.yaml             # Release builds (binaries, archives, deb/rpm, images)
└── .github/workflows/           # CI, CodeQL, release, apt/rpm Pages publishing
```

---

## Key Package Responsibilities

### `internal/master/agentgw`

The Agent Gateway is the Master's agent-facing half. It:
- Terminates mTLS and maps the client-certificate CN to the agent ID (`MTLSMiddleware`), rejecting
  certificates that are revoked or not signed by the internal CA.
- Handles `Enroll` (bootstrap token → signed cert) and `RenewCert`.
- Maintains an in-memory session registry `agentID → session`, tracks heartbeats, and marks agents
  offline when they stop reporting.
- Correlates request/response pairs over the bidi-stream via `request_id` — used today by the
  federated metrics fetch.

### `internal/master/reconciler`

Builds the desired state per server from the `assignments` table and pushes `ApplyDesiredState` to
every connected agent — on a ticker (15 s) and immediately after a mutation via `PushNow`.

### `internal/agent/compose`

Implements the **Converge Engine** — the most complex package in the codebase. See
[04-reconciliation.md](04-reconciliation.md) for the algorithm and the supported-fields matrix.

### `internal/shared/compose`

The single compose validator, used both by the Master's `ValidateCompose` RPC (editor diagnostics)
and by the Agent. Keeping it shared is what stops the editor and the deploy path from disagreeing
about which fields orkestra supports.

### `internal/master/secrets`

Houses the built-in secret sealing helpers (`Seal`/`Open`, XChaCha20-Poly1305 + KEK). See
[05-secrets.md](05-secrets.md).

---

## Build Tooling

### Prerequisites

```
go      >= 1.24
buf     >= 1.30      (protobuf codegen)
node    >= 20        (web UI build only)
sqlc    >= 1.26      (SQL → Go codegen)
goose                (DB migrations: go install github.com/pressly/goose/v3/cmd/goose@latest)
goreleaser           (release builds)
```

The backend build needs only Go and buf; Node is required only to build the web UI.

### Makefile Targets

| Target | Action |
|---|---|
| `make proto` | `buf generate` — regenerates Go + TS from the `.proto` files |
| `make sqlc` | `sqlc generate` — regenerates the DB layer from the SQL queries |
| `make web` | `npm ci && npm run build` — builds the React SPA into `web/dist/` |
| `make build` | Builds both binaries into `bin/` (embeds `web/dist` if present) |
| `make build-dev` | Builds with the `dev` tag — no embed; the Master proxies to Vite on `:5173` |
| `make test` | `go test ./...` |
| `make test-integration` | `go test -tags integration` — requires a running Docker daemon |
| `make lint` | `golangci-lint run` + `buf lint` |
| `make vet` | `go vet ./...` |
| `make migrate` / `migrate-down` / `migrate-status` | goose against `MIGRATE_DSN` |
| `make dev-master` / `make dev-agent` | Build with the `dev` tag and run locally at debug level |
| `make release` / `make release-snapshot` | goreleaser release / dry-run |

### protobuf / buf

`buf.gen.yaml` drives two plugin pairs:
1. `protoc-gen-go` + `protoc-gen-connect-go` → `internal/shared/gen/`
2. `protoc-gen-es` + `protoc-gen-connect-es` → `web/gen/`

Both generated directories are gitignored and regenerated via `make proto` locally or in CI before
the build step. The sqlc output under `internal/master/store/` is committed.

### Embedding the Web UI

`web/ui_prod.go` embeds `web/dist` via `go:embed` and serves it with an SPA fallback, so the Master
is a single artifact serving both API and UI. Under the `dev` build tag, `web/ui_dev.go` replaces
the embedded FS with a reverse proxy to `http://localhost:5173` — UI changes are then instant
without rebuilding Go.

### Release

`goreleaser` produces:
- `orkestra-master` and `orkestra-agent` binaries for linux/amd64 and linux/arm64.
- `.tar.gz` archives plus `.deb`/`.rpm` packages (systemd unit, default env file, system user).
- Multi-arch Docker images `ghcr.io/heckertobias/orkestra-master` and `…/orkestra-agent`.
- A checksum file.

A separate GitHub Pages workflow publishes the signed apt/rpm repository — see
[08-deployment.md](08-deployment.md).
