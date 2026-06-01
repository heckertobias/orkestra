# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

orkestra is a lightweight orchestrator for Docker/Compose hosts — a Master/Agent system that
centrally manages self-healing Compose stacks across Linux servers (a simpler alternative to
Kubernetes/Nomad). The `docs/` directory holds the authoritative design; `docs/00-overview.md`
and `docs/01-repo-layout.md` are the best entry points.

**Project status:** early development (Milestone 0 — scaffold). Most `internal/` packages
described in `docs/01-repo-layout.md` (agentgw, reconciler, pki, auth, agent/*, web/) are
*planned but not yet implemented*. Only `internal/master/store` and `internal/shared/version`
exist today. Treat the docs as the target design, not the current state. `docs/09-roadmap.md`
tracks milestones.

## Common commands

```bash
make build        # build both binaries into bin/ (embeds web/dist if present)
make build-dev    # build with `dev` tag — no web embed; Master proxies to Vite :5173
make test         # go test ./...
make test-integration  # go test -tags integration (requires a running Docker daemon)
make lint         # golangci-lint run + buf lint
make vet          # go vet ./...

# Run a single test:
go test ./internal/master/store/ -run TestName -v
```

## Code generation — run after editing schemas

Generated code is committed to the repo. After changing the relevant source you MUST regenerate:

- Edit any `proto/orkestra/v1/*.proto` → `make proto` (`buf generate`). Outputs Go to
  `internal/shared/gen/` and TypeScript Connect clients to `web/gen/`. Lint protos with `buf lint`.
- Edit SQL in `internal/master/store/queries/*.sql` or the migrations → `make sqlc`
  (`sqlc generate`). Outputs type-safe Go into `internal/master/store/`. Config: `sqlc.yaml`.
- The backend build needs only Go + buf; Node is only required to build the web UI.

## Database migrations

SQLite (CGo-free `modernc.org/sqlite`, WAL mode), versioned with `goose`. Migrations live in
`internal/master/store/migrations/`.

```bash
make migrate          # apply pending migrations (MIGRATE_DSN env or orkestra-dev.db)
make migrate-down     # roll back the last migration
make migrate-status
```

When adding a migration, also add/adjust queries in `internal/master/store/queries/` and rerun
`make sqlc`. `00001_initial.sql` defines the full schema: PKI (`ca`, `certificates`,
`enrollment_tokens`), users/auth (`users`, `sessions`, `roles`, `role_bindings`, `oidc_config`),
`servers`/`agent_state`, and the desired-state core (`stacks` → `stack_versions` → `assignments`),
plus `secrets`/`secret_bindings`, `audit_log`, and `events`.

## Architecture (big picture)

Two binaries, one shared protobuf schema (`cmd/orkestra-master`, `cmd/orkestra-agent`):

- **Master** holds the single source of truth (Desired State) in SQLite, runs an internal CA/PKI,
  and serves the Web UI. It never dials out to Agents.
- **Agent** runs on each server, controls Docker via the Engine SDK + `compose-go`, and
  reconciles actual container state toward the Master's desired state.
- **Agents connect outbound** to the Master over a long-lived mTLS gRPC bidi-stream
  (NAT/firewall friendly). Enrollment (bootstrap token → signed cert) is a one-time bootstrap.

**RPC:** ConnectRPC (`connectrpc.com/connect`) serves one schema two ways — gRPC bidi-streams for
Agent↔Master (`agent.proto`), and the Connect protocol (JSON/binary + server-streaming) for the
browser SPA (`stacks.proto`, `secrets.proto`, `auth.proto`). No gRPC-web proxy needed. See
`docs/02-protocol.md`.

**Desired State & reconciliation** (`docs/04-reconciliation.md`): a server's desired state is the
union of its `assignments`, each binding a `stack_version` (compose YAML + env + secret refs) to a
`desired_status` (running/stopped/removed). The Master pushes the *full* desired state per server
(not diffs) so reconnects are safe. The Agent's Converge Engine (`internal/agent/compose`,
planned) re-implements `docker compose up/down/recreate` on top of `compose-go` (which only
parses, no orchestration) — this is the highest-complexity package; container identity and
recreate decisions hinge on an `orkestra.spec-hash` label. Only a documented subset of Compose
fields is supported; unsupported fields fail loudly.

**Streaming:** the Master bridges browser server-streams (logs/stats/events) to Agent streams via
a per-agent stream mux keyed by `stream_id`, with backpressure propagated to the Agent.

**Web UI:** React/TS/Vite SPA in `web/` (planned), built to `web/dist/` and embedded into the
Master binary via `go:embed`. The `dev` build tag swaps the embed for a proxy to the Vite dev
server.

## Conventions & gotchas

- **Module path is `github.com/egt/orkestra`** and **runtime env vars use the `DOCKESTRA_` prefix**
  (e.g. `DOCKESTRA_UI_ADDR`, `DOCKESTRA_AGENT_ADDR`, `DOCKESTRA_AGENT_DATA`, `DOCKESTRA_MASTER_KEY`).
  These are pre-rename leftovers — keep them consistent with existing code; don't rename
  piecemeal.
- The agent binary is subcommand-based: `orkestra-agent serve|enroll`. The master takes flags only.
- Default ports: `8443` Agent gRPC (mTLS, HTTP/2), `8080` UI/API, `9090` Prometheus metrics.
- Structured logging via stdlib `log/slog`; version info is injected at build time via `-ldflags`
  into `internal/shared/version`.
- Secrets at rest are encrypted with a KEK derived from `DOCKESTRA_MASTER_KEY`; the CA private key
  and secret ciphertext are never stored in plaintext.
