# orkestra — Repository Layout & Build Tooling

## Directory Structure

```
orkestra/
├── cmd/
│   ├── orkestra-master/        # Master entrypoint (main.go)
│   └── orkestra-agent/         # Agent entrypoint (main.go)
├── proto/
│   └── orkestra/v1/
│       ├── agent.proto          # Agent↔Master stream + Enroll RPC
│       ├── stacks.proto         # Stacks/Server/Deployment API (UI)
│       ├── secrets.proto        # Secret CRUD API
│       ├── auth.proto           # User auth, sessions, OIDC
│       └── common.proto         # Shared message types
├── internal/
│   ├── master/
│   │   ├── agentgw/             # Agent Gateway: gRPC server, stream mux, session registry
│   │   ├── api/                 # Connect handlers for the UI API
│   │   ├── reconciler/          # Desired-State diff → commands to Agents
│   │   ├── store/               # SQLite (sqlc-generated) + repositories
│   │   ├── pki/                 # Internal CA, cert issuance, bootstrap tokens
│   │   ├── auth/                # Sessions, local users, OIDC, RBAC middleware
│   │   ├── secrets/             # SecretProvider interface + builtin + openbao
│   │   └── audit/               # Audit log writer
│   ├── agent/
│   │   ├── conn/                # gRPC client, reconnect/backoff, mTLS
│   │   ├── enroll/              # Bootstrap enrollment (token → cert)
│   │   ├── dockerctl/           # Docker SDK wrapper (containers, images, networks, volumes)
│   │   ├── compose/             # compose-go loader + Converge Engine
│   │   ├── reconcile/           # Local reconcile loop against Desired State
│   │   ├── secrets/             # Secret materialization (tmpfs / Docker secret / env)
│   │   └── telemetry/           # Status / log / stats reporter
│   └── shared/
│       ├── gen/                 # Generated protobuf code (Go)
│       └── version/             # Build-time version info
├── web/                         # React SPA (Vite + TypeScript)
│   ├── src/
│   └── gen/                     # Generated TypeScript Connect clients (buf)
├── deploy/
│   ├── systemd/                 # orkestra-master.service, orkestra-agent.service
│   ├── docker/                  # Dockerfiles + Compose setup for self-hosting the Master
│   └── install-agent.sh         # Bootstrap installer (downloads binary, enrolls, installs service)
├── docs/                        # Design documentation (this directory)
├── buf.yaml                     # buf module config
├── buf.gen.yaml                 # buf codegen config (Go + TypeScript plugins)
├── Makefile                     # Developer shortcuts
├── .goreleaser.yaml             # Release builds (amd64/arm64 binaries + Docker images)
└── .github/workflows/           # CI/CD pipeline (GitHub Actions)
```

---

## Key Package Responsibilities

### `internal/master/agentgw`

The Agent Gateway is the heart of the Master. It:
- Accepts TLS connections from Agents (verifying client certs against the internal CA).
- Maintains an in-memory session registry: `agentID → stream`.
- Multiplexes request/response over the bidi-stream using `request_id` correlation.
- Exposes a `Send(agentID, MasterMessage) → AgentMessage` abstraction to the Reconciler and API
  handlers — they don't deal with raw streams.

### `internal/master/reconciler`

Watches the SQLite `assignments` table (via polling or WAL hook). When a stack version or
assignment changes, it computes the new Desired State for all affected servers and pushes
`ApplyDesiredState` messages via the Agent Gateway.

### `internal/agent/compose`

Implements the **Converge Engine** — the most complex package. See
[04-reconciliation.md](04-reconciliation.md) for the algorithm and the supported-fields matrix.

### `internal/master/secrets`

Houses the `SecretProvider` interface and both implementations (`builtin`, `openbao`).
See [05-secrets.md](05-secrets.md).

---

## Build Tooling

### Prerequisites

```
go      >= 1.24
buf     >= 1.30      (protobuf codegen)
node    >= 20        (web UI build)
sqlc    >= 1.26      (SQL → Go codegen)
goose                (DB migrations: go install github.com/pressly/goose/v3/cmd/goose@latest)
goreleaser           (release builds)
```

### Makefile Targets

| Target | Action |
|---|---|
| `make proto` | `buf generate` — regenerates Go + TS from `.proto` files |
| `make sqlc` | `sqlc generate` — regenerates DB layer from SQL queries |
| `make web` | `cd web && npm run build` — builds React SPA into `web/dist/` |
| `make build` | `go build ./cmd/...` — builds both binaries (embeds web/dist) |
| `make test` | `go test ./...` |
| `make lint` | `golangci-lint run` + `buf lint` |
| `make dev-master` | Runs Master with hot-reload (via `air`) |
| `make dev-agent` | Runs Agent pointed at local Master |
| `make migrate` | Runs pending DB migrations against the dev SQLite file |
| `make release` | `goreleaser release --clean` |

### protobuf / buf

`buf.gen.yaml` drives two plugins:
1. `protoc-gen-go` + `protoc-gen-connect-go` → `internal/shared/gen/`
2. `protoc-gen-es` + `protoc-gen-connect-es` → `web/gen/`

Both directories are gitignored and regenerated via `make proto` / `make sqlc` (locally) or in
CI before the build step. The backend build needs only Go + buf (no Node needed for the backend).

### Embedding the Web UI

`internal/master/api/embed.go`:
```go
//go:build !dev

package api

import "embed"

//go:embed ../../../web/dist
var webFS embed.FS
```

In dev mode (build tag `dev`), the Master proxies to the Vite dev server (`localhost:5173`).

### Release

`goreleaser` produces:
- `orkestra-master_{os}_{arch}` and `orkestra-agent_{os}_{arch}` for linux/amd64 and linux/arm64.
- Docker images `ghcr.io/heckertobias/orkestra-master:{version}`.
- A checksum file and systemd unit files as release assets.
