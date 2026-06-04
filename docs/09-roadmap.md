# orkestra — Implementation Roadmap

## Milestones

### M0 — Repo Scaffolding (Foundation)

**Goal:** A buildable, testable skeleton with all tooling wired up.

- [x] Go module init (`go mod init github.com/heckertobias/orkestra`)
- [x] Directory structure as per [01-repo-layout.md](01-repo-layout.md)
- [x] `buf.yaml` + `buf.gen.yaml` (Go + TS codegen plugins)
- [x] Skeleton `.proto` files (services declared, messages stubbed)
- [x] First `buf generate` pass → `internal/shared/gen/` + `web/gen/` (gitignored; CI regenerates via `make proto` before every build)
- [x] PostgreSQL setup: pgx/v5, goose migrations directory
- [x] First migration: full schema (all tables)
- [x] `sqlc.yaml` + SQL query files
- [x] `Makefile` with `proto`, `sqlc`, `build`, `test`, `lint`, `web`, `migrate` targets
- [x] `cmd/orkestra-master/main.go` — starts HTTP server, serves `GET /healthz`
- [x] `cmd/orkestra-agent/main.go` — starts, logs "waiting for enrollment"
- [x] `go test ./...` passes (no tests yet, but no compilation errors)
- [x] `.github/workflows/ci.yml` (GitHub Actions, build + test + lint)
- [x] `web/` Vite + React + TypeScript scaffold (`npm create vite`)

**Result:** ✅ Both binaries compile and the CI pipeline is green.

---

### M1 — PKI, Enrollment & Persistent mTLS Connection

**Goal:** An Agent can enroll and maintain a persistent authenticated connection to the Master.

- [x] `internal/master/keys/`: `KeySource` interface + `file` (default) and `env` (dev/warn) implementations
- [x] `internal/master/pki/`: CA generation on first start, KEK-encrypted storage (via `KeySource`), CSR signing
- [x] `internal/master/store/`: full schema migration, sqlc queries for servers + enrollment_tokens + certificates
- [x] `AgentService.Enroll` RPC handler (token validation, CSR signing, server record creation)
- [x] `AgentService.Connect` RPC handler (mTLS verification, session registry registration)
- [x] `internal/master/agentgw/`: session registry (`agentID → stream`), Hello processing, heartbeat/offline detection
- [x] `internal/agent/enroll/`: keypair generation, CSR creation, Enroll RPC call, cert persistence
- [x] `internal/agent/conn/`: mTLS dial, `Connect` stream, exponential backoff reconnect, Hello send
- [x] Agent sends periodic `StatusReport` (empty stacks list for now) as heartbeat
- [x] Master marks server `online` on Hello, `offline` after 3 missed heartbeats
- [x] `install-agent.sh` basic version (enroll + systemd install)
- [x] `make dev-agent` works against local Master

**Result:** ✅ `orkestra-agent enroll --master ... --bootstrap-token ...` completes; agent appears online in Master logs.

---

### M2 — Container Control & Minimal Web UI

**Goal:** Manage individual containers on remote servers via the browser.

- [x] `internal/agent/dockerctl/`: ContainerList, ContainerStart, ContainerStop, ContainerRestart, ContainerRemove, ImagePull
- [x] `internal/agent/telemetry/`: LogStream (bridge Docker logs to LogChunk messages), StatsStream (bridge docker stats to StatsChunk)
- [x] Master bridges `StreamLogs` + `StreamStats` requests: browser → Master → Agent → Master → browser (stub, full bridging in integration pass)
- [x] `AgentService.Connect`: handle `ExecCommand` (start/stop/restart/pull/rm) dispatch, return `CommandResult`
- [x] `StackService.ListServers`, `StackService.GetServer` — query from DB + merge with agentgw session state
- [x] **Web UI — Vite + React + TypeScript scaffold**, Tailwind v4, dark theme with brand colour tokens
- [ ] **Logo & brand assets:** SVG logo variants (full illustration, head icon, wordmark) in `web/src/assets/`; favicon + app icon derived from head icon
- [x] **Web UI — Server List page:** server cards with online/offline status
- [x] **Web UI — Server Detail page:** container table, start/stop/restart/pull/remove actions
- [ ] **Web UI — Live Logs drawer:** streaming log output with follow toggle (M6 stream bridging)
- [ ] **Web UI — Live Stats:** CPU/memory bar chart per container (M6 stream bridging)

**Result:** ✅ Both binaries build; Server List and Server Detail pages render; container actions dispatch to Agent.

---

### M3 — Compose Stacks & Desired-State Reconciliation

**Goal:** Deploy, update, and roll back Compose stacks declaratively with self-healing.

- [x] Full schema migration: `stacks`, `stack_versions`, `assignments`, `agent_state`
- [x] `StackService` CRUD: CreateStack, UpdateStack (→ new version), ListStackVersions, AssignStack, UnassignStack, RollbackStack
- [x] `internal/agent/compose/`: compose-go Loader, Converge Engine (MVP field matrix)
  - [x] LoadProject from YAML + env vars via compose-go
  - [x] `specHash` for container identity / recreate decisions (`orkestra.spec-hash` label)
  - [x] create / recreate / remove actions with label-based container tracking
  - [x] network + volume binding support (bind mounts, port mappings)
  - [ ] healthcheck polling for `condition: service_healthy` (planned)
- [x] `internal/agent/reconcile/`: reconcile loop (on ApplyDesiredState + periodic 30s resync)
- [x] `internal/master/reconciler/`: polls assignments every 15s, pushes ApplyDesiredState to connected Agents; PushNow() on mutations
- [ ] Drift detection: agent reports `drift_detected` + description in `StatusReport` (planned)
- [x] **Web UI — Stacks List page** (with Create dialog)
- [x] **Web UI — Stack Detail page** (version history, YAML viewer)
- [ ] **Web UI — Stack Create/Edit dialog** with Monaco editor (planned)
- [ ] Integration tests for Converge Engine (against real Docker daemon via `dind` in CI)

**Result:** ✅ Create a Compose stack in the UI → assigned to a server → reconciler pushes desired state → Agent converges containers.

---

### M4 — Secrets (builtin + OpenBao)

**Goal:** Secrets managed centrally, distributed securely, never persisted in plaintext.

- [x] Full secrets schema migration: `secrets`, `secret_bindings`
- [x] `internal/master/secrets/`: builtin provider (XChaCha20-Poly1305 + KEK via pki.Encrypt/Decrypt)
- [x] `SecretService` CRUD: CreateSecret, UpdateSecret, DeleteSecret, ListSecrets, GetSecret, RevealSecret
- [ ] MigrateProvider (OpenBao integration — M6+)
- [ ] Secret resolution in `ApplyDesiredState` (Master resolves → sends over mTLS)
- [ ] `internal/agent/secrets/`: materialization (ENV, FILE/tmpfs, DOCKER_SECRET with Swarm fallback)
- [ ] Secret cleanup on stack STOPPED/REMOVED
- [x] **Web UI — Secrets page** (provider toggle, masked values, reveal with re-auth)
- [ ] **Web UI — Secret binding editor** in Stack Create/Edit dialog
- [x] Audit logging for all secret operations

**Result:** ✅ Create a builtin secret in the UI → encrypted with KEK, stored in Postgres; reveal requires re-auth.

---

### M5 — User Auth, Sessions & RBAC

**Goal:** The system is protected: authenticated access only, actions gated by role.

- [x] First-run setup flow (one-time setup URL in logs → POST /api/setup)
- [x] Local user auth: argon2id, session token, httponly cookie
- [x] `sessions` table + session middleware (HTTP middleware + Connect interceptor)
- [x] RBAC: `roles`, `role_bindings` tables; RBAC Connect interceptor
- [x] Seeded roles: `admin`, `operator`, `viewer`
- [x] `AuthService`: Login, Logout, GetCurrentUser, ListUsers, CreateUser, UpdateUser, AssignRole, RevokeRole, ListRoleBindings
- [x] **Web UI — Login page** (local auth + first-run setup form)
- [x] **Web UI — Users & Roles page** (admin only for create/assign)
- [x] Audit log for all auth events (login, logout, role change, user create)
- [x] **Web UI — Audit Log page**

**Result:** ✅ Unauthenticated access returns 401. Admin can create users and assign roles. All auth actions logged.

---

### M6 — OIDC, Metrics, Event Feed & Polish

**Goal:** Production-ready features and UX polish.

- [ ] OIDC provider integration (`coreos/go-oidc`), claim→role mapping, `oidc_config` table
- [ ] **Web UI — OIDC configuration tab** + SSO login button
- [ ] Prometheus metrics (Master + Agent) as described in [08-deployment.md](08-deployment.md)
- [ ] `/healthz` + `/readyz` endpoints
- [ ] `events` table + live `StreamEvents` feed in UI (Dashboard event panel)
- [ ] **Web UI — Dashboard** (fleet stats, event feed)
- [ ] Compose field matrix extended (scale/replicas, additional fields from user feedback)
- [ ] Rate limiting on auth endpoints
- [ ] API key auth (for non-browser clients / CI scripts)
- [ ] UI polish: error states, loading skeletons, empty states, toasts for async actions

**Result:** SSO works; Prometheus scrapes cleanly; Dashboard shows live event feed.

---

### M7 — Hardening, Packaging & Release

**Goal:** Release-ready, documented, and operationally solid.

- [ ] Cert rotation: Agent auto-renews cert 30 days before expiry via `RenewCert` RPC
- [ ] Cert revocation: Master revokes cert + disconnects agent; Agent re-enrolls
- [ ] `goreleaser` configuration: binaries + archives + Docker images (amd64 + arm64)
- [ ] `deploy/install-agent.sh` final version (checksum verification, multi-distro systemd)
- [ ] `deploy/systemd/` final unit files (hardened: NoNewPrivileges, ProtectSystem, etc.)
- [ ] `deploy/docker/compose.yaml` with healthcheck, restart policy, volume config
- [ ] Security checklist review (see [06-security-auth.md](06-security-auth.md))
- [ ] PostgreSQL `pg_dump` backup documentation
- [ ] Full documentation review + CHANGELOG
- [ ] v0.1.0 release tag

**Result:** `install-agent.sh --master ... --bootstrap-token ...` on a fresh Linux server gives a fully working agent in < 60 seconds. `docker compose up -d` gives a working Master.

---

## Testing Strategy

| Test Type | Scope | Tools |
|---|---|---|
| Unit | Store queries, RBAC logic, spec-hash computation, secret encryption | `go test` |
| Integration (DB) | Postgres migrations, sqlc queries against real Postgres | `go test` + testcontainers-go (or `services: postgres` in CI) |
| Integration (Docker) | Converge Engine end-to-end | `go test` + Docker daemon (dind in CI) |
| Integration (gRPC) | Enrollment + Connect stream (in-process with TLS) | `go test` + `net/http/httptest` |
| E2E (manual) | Full flow per Verification section | Manual / future: Playwright |

The most critical integration test is the **Converge Engine** test, which:
1. Starts a real Docker daemon.
2. Sends an `ApplyDesiredState` with a known Compose definition.
3. Verifies the containers are created with the correct spec (labels, env, ports).
4. Mutates external state (stops a container).
5. Runs another reconcile.
6. Verifies the container is restored.

This test lives in `internal/agent/compose/converge_integration_test.go` and requires
`//go:build integration` to avoid running on every `go test ./...`.
