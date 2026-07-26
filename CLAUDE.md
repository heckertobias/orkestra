# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

orkestra is a lightweight orchestrator for Docker/Compose hosts — a Master/Agent system that
centrally manages self-healing Compose stacks across Linux servers (a simpler alternative to
Kubernetes/Nomad).

**`docs/` is the authoritative description of the system.** It is written as the target behaviour
of the current feature set — open bugs are *not* reflected there, they live in GitHub issues (see
the issue map below). Read the relevant chapter before changing a subsystem:

| Doc | Covers |
|---|---|
| [docs/00-overview.md](docs/00-overview.md) | Architecture, tech stack, core principles |
| [docs/01-repo-layout.md](docs/01-repo-layout.md) | Directory tree, package responsibilities, build tooling |
| [docs/02-protocol.md](docs/02-protocol.md) | Protobuf schema, connection lifecycle, streaming, ports |
| [docs/03-data-model.md](docs/03-data-model.md) | Full PostgreSQL schema and its design decisions |
| [docs/04-reconciliation.md](docs/04-reconciliation.md) | Desired state, Converge Engine, spec-hash, Compose support matrix |
| [docs/05-secrets.md](docs/05-secrets.md) | Built-in secret store, sealing, bindings |
| [docs/06-security-auth.md](docs/06-security-auth.md) | PKI/mTLS, sessions, OIDC, RBAC, audit, KEK |
| [docs/07-web-ui.md](docs/07-web-ui.md) | Routes, pages, frontend stack, branding |
| [docs/08-deployment.md](docs/08-deployment.md) | Observability, packaging, ingress, backup/recovery |
| [docs/09-updates.md](docs/09-updates.md) | Fleet update layers, policy model, reported availability |

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

# Frontend (in web/):
npm run build && npm run lint && npm test   # tsc + vite build, eslint, vitest
```

## Code generation — run after editing schemas

Generated directories (`internal/shared/gen/`, `web/gen/`) are gitignored and regenerated locally
or in CI. The sqlc output in `internal/master/store/*.sql.go` **is** committed. After changing the
relevant source you MUST regenerate:

- Edit any `proto/orkestra/v1/*.proto` → `make proto` (`buf generate`). Outputs Go to
  `internal/shared/gen/` and TypeScript Connect clients to `web/gen/`. Lint protos with `buf lint`.
- Edit SQL in `internal/master/store/queries/*.sql` or the migrations → `make sqlc`
  (`sqlc generate`). Outputs type-safe Go into `internal/master/store/`. Config: `sqlc.yaml`.
- The backend build needs only Go + buf; Node is only required to build the web UI.

## Database migrations

PostgreSQL via `pgx/v5`, versioned with `goose`. Migrations live in
`internal/master/store/migrations/`, are embedded in the Master binary, and are applied
automatically at startup.

```bash
make migrate          # apply pending migrations (MIGRATE_DSN env or default local Postgres DSN)
make migrate-down     # roll back the last migration
make migrate-status
```

When adding a migration, also add/adjust queries in `internal/master/store/queries/` and rerun
`make sqlc`. The effective schema (all migrations folded together) is documented in
[docs/03-data-model.md](docs/03-data-model.md) — update it in the same change.

## Architecture (big picture)

Two binaries, one shared protobuf schema (`cmd/orkestra-master`, `cmd/orkestra-agent`):

- **Master** holds the single source of truth (desired state) in PostgreSQL, runs an internal
  CA/PKI, and serves the Web UI. It never dials out to Agents.
- **Agent** runs on each server, controls Docker via the Engine SDK + `compose-go`, and reconciles
  actual container state toward the Master's desired state.
- **Agents connect outbound** over a long-lived mTLS gRPC bidi-stream (NAT/firewall friendly).
  Enrollment (bootstrap token → signed cert) is a one-time bootstrap; the Master assigns the agent
  ID and uses it as the cert CN.

**RPC:** ConnectRPC serves one schema two ways — gRPC bidi-streams for Agent↔Master
(`agent.proto`) and the Connect protocol for the browser SPA (`stacks.proto`, `secrets.proto`,
`auth.proto`). Details: [docs/02-protocol.md](docs/02-protocol.md).

**Desired state:** a server's desired state is the union of its `assignments`, each binding a
`stack_version` (compose YAML + declared env-var *names* + secret refs) to a `desired_status`.
Env-var **values** live on the assignment, not the version. The Master pushes the *full* desired
state per server (never diffs), so reconnects are safe. The Agent's Converge Engine
(`internal/agent/compose`) re-implements `docker compose up/down/recreate` on top of `compose-go`;
container identity and recreate decisions hinge on the `orkestra.spec-hash` label. Only a
documented subset of Compose fields is supported —
[docs/04-reconciliation.md](docs/04-reconciliation.md) has the matrix, and
`internal/shared/compose` is the shared validator behind both the editor and the agent.

**Web UI:** React/TS/Vite SPA in `web/`, built to `web/dist/` and embedded into the Master binary
via `go:embed` (`web/ui_prod.go`). The `dev` build tag swaps the embed for a proxy to Vite.

## What is not wired yet

Useful context before hunting for "missing" code — these are known gaps, not bugs:

- **Live logs/stats and container commands.** `StreamLogs`/`StreamStats` return `Unimplemented`;
  the agent-side streamers exist but are not dispatched; `ExecCommand` is not handled by the agent.
- **Secret delivery.** Secrets can be stored and revealed, but `secret_refs` is written empty and
  the reconciler does not resolve secrets into `ApplyDesiredState`.
- **Agent state / inventory.** `agent_state` is never written, so container inventory and drift
  reporting in the UI stay empty.
- **Fleet updates.** Only the schema and the Master-side persistence of reported availability.
- **Audit coverage.** Only auth/user and secret actions are audited; stack/server/role mutations
  are not.

## Where to look on GitHub

Issues are grouped by milestone (**Beta** → **1.0** → **Post-1.0**) and labelled by area
(`area/agent`, `area/master`, `area/web`, `area/secrets`, `area/build`) and type (`bug`,
`enhancement`, `idea`, `roadmap`). Quick lookup by topic:

| Topic | Issues |
|---|---|
| **Open bugs** | #70 fields silently dropped · #71 `restart: on-failure:N` · #72 removed stack keeps containers · #73 pull after recreate decision · #75 `ExecOnContainer` false success · #76 agent env leaks into interpolation · #77 unset metrics · #50 permission matrix drops role |
| Compose field support | #10 networks · #11 named/tmpfs volumes · #12 `depends_on`/health · #13 wider fields · #14 scale · #15 build · #16 `network_mode` · #53 `configs` · #54 profiles · #55 resources/GPU · #56 `container_name` |
| Converge internals | #17 registry auth · #18 spec-hash gaps · #57 support-table single source of truth · #58 JSON-schema validation · #65 master-side dry-run · #81 fail on unresolved `${VAR}` |
| Live streams & commands | #19 logs · #20 stats · #21 exec/terminal · #61 command dispatch · #62 per-service actions · #66 extended container actions |
| Inventory, drift, resources | #29 populate `agent_state` · #28 drift reporting · #59 resource inventory · #60 targeted delete · #34 prune · #63 forward Docker events |
| Secrets | #22 deliver to deployments · #23 OpenBao backend · #33 rotation & history |
| Auth & security | #83 explicit CSRF defence · #84 rate-limit all public endpoints + trusted client IP · #85 audit coverage for stack/server/admin mutations · #35 revocation propagation · #36 metrics endpoint auth |
| Master & API | #30 pagination · #78 audit/event retention · #79 offline + `DeleteServer` semantics · #80 master/agent version compatibility · #32 backend test coverage |
| Web UI | #46 route preloading · #47 component tests · #48 use generated Connect clients · #49 Playwright E2E |
| Updates & fleet ops | #9 update system · #31 backup & restore · #82 disaster-recovery runbook |
| Packaging & keys | #24 interactive KeySource · #25 KMS KeySource · #26 more packaging channels · #27 reproducible builds |
| Larger ideas | #64 multi-file stacks · #67 start-first rollout · #68 notifications · #69 import existing stacks · #86 direct TLS without a reverse proxy |

```bash
gh issue list --label bug --state open        # current defects
gh issue list --milestone Beta --state open   # what's next
gh issue view 70                              # details of one issue
```

## Conventions & gotchas

- **Module path is `github.com/heckertobias/orkestra`** and **runtime env vars use the `ORKESTRA_`
  prefix** (e.g. `ORKESTRA_UI_ADDR`, `ORKESTRA_AGENT_ADDR`, `ORKESTRA_METRICS_ADDR`,
  `ORKESTRA_AGENT_DATA`, `ORKESTRA_DATABASE_URL`, `ORKESTRA_MASTER_KEY_FILE`,
  `ORKESTRA_SECURE_COOKIES`, `ORKESTRA_PUBLIC_URL`, `ORKESTRA_AGENT_TLS_SANS`).
- The agent binary is subcommand-based: `orkestra-agent serve|enroll`. The master takes flags only.
- Default ports: `4440` Agent gRPC (mTLS, HTTP/2; 4440 = orchestra concert pitch A440), `8080`
  UI/API, `9090` Master metrics, `9091` Agent metrics (federated through the Master, never scraped
  directly).
- The Master serves the UI over **plain HTTP** — TLS for browsers is terminated by a reverse proxy.
  `:4440` is the opposite: it must reach the Master undecrypted, because agents pin the internal CA.
- Docker access goes through `github.com/moby/moby/client` (not `docker/docker`).
- Structured logging via stdlib `log/slog`; version info is injected at build time via `-ldflags`
  into `internal/shared/version`.
- The KEK (CA key, secret values, OIDC client secret, SMTP password) is loaded via a pluggable
  `KeySource` (`internal/master/keys/`). Default: `ORKESTRA_MASTER_KEY_FILE` → file/secret-mount.
  Dev only: `ORKESTRA_MASTER_KEY` (logs a startup warning). The KEK **must** live in a different
  trust domain from the DB credentials — never in the same `.env` or Compose `environment:` block.
  See [docs/06-security-auth.md](docs/06-security-auth.md) § "KEK & KeySource".

## Contributing workflow

- Work on a feature branch and open a PR into `main` (the branch ruleset requires it).
- Use the repository's PR and issue templates when they exist.
- All outward-facing text — commits, PR bodies, issues, code comments, docs — is written in
  English.
- Keep `docs/` in sync with behaviour changes; that is where the current state is recorded.
