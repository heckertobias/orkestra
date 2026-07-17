# orkestra — Desired State & Reconciliation

## Model

The **Desired State** of a server is the union of all active `assignments` for that server:
each assignment binds a `stack_version` (with its `compose_yaml`, `env_vars`, `secret_refs`) to a
server with a `desired_status` (running | stopped | removed).

The Master computes and distributes this state; the Agent **reconciles** the actual Docker state
toward it.

---

## Master-Side: Triggering Reconciliation

The Master pushes `ApplyDesiredState` to an Agent whenever:

1. A new assignment is created or updated (deploy, rollback).
2. An assignment's `desired_status` changes.
3. An assignment is deleted.
4. An Agent reconnects after being offline (Master always re-pushes full desired state on reconnect).
5. A periodic push (the reconciler re-pushes the full desired state to connected agents every ~15 s).

The message contains the **full** desired state for that server (not a diff) — this makes it
safe to re-send on reconnect without any missed-delta issues.

---

## Agent-Side: Reconcile Loop

The reconcile loop runs:
- Immediately on receiving `ApplyDesiredState` from the Master.
- Periodically every 30 seconds (resync against Docker actual state, catches external drift).

### Algorithm per Stack

This reflects the current implementation (`internal/agent/compose/converge.go`). Planned
extensions (dependency ordering, health gating, secret materialization, network/volume creation)
are tracked in [ROADMAP.md](../ROADMAP.md#2-converge-engine--compose-coverage).

```
function reconcile(desiredStack StackDesiredState):

  if desiredStack.status == REMOVED:
    stop + remove all containers with label orkestra.stack-id=<stack_id>
    return

  if desiredStack.status == STOPPED:
    stop (but keep) all containers with label orkestra.stack-id=<stack_id>
    return

  project = compose_go.Load(
    compose_yaml  = desiredStack.compose_yaml,
    env_overrides = desiredStack.env_vars,
  )
  // project: types.Project (parsed service graph, no orchestration)

  actual = docker.ContainerList(filter: label orkestra.stack-id=<stack_id>)

  // Services are processed in stable alphabetical order (no depends_on ordering yet).
  for service in sorted(project.services):
    hash = spec_hash(service)
    cur  = actual[service.name]

    if cur exists AND cur.spec-hash == hash AND cur.state == "running":
      keep                                   // up-to-date, nothing to do
    else:
      if cur exists: stop + remove cur       // drifted or not running → recreate
      ensure_image(service.image, service.pull_policy)  // pull per pull_policy
      id = docker.ContainerCreate(spec_from_service(service))
      docker.ContainerStart(id)

  // Any managed container whose service is no longer in the project is removed.
  for orphan in actual not in project.services:
    stop + remove orphan

  report StatusReport to Master
```

### Container Identity & Idempotency

Every orkestra-managed container carries these labels:

| Label | Value |
|---|---|
| `orkestra.managed` | `true` |
| `orkestra.stack-id` | `<stack_id>` |
| `orkestra.service` | `<compose_service_name>` |
| `orkestra.spec-hash` | SHA-256 (truncated) of the normalized service spec |
| `com.docker.compose.project` / `com.docker.compose.service` | for `docker compose ls`/tooling compatibility |

The **`spec-hash`** determines whether a `recreate` is needed. It is currently computed as a
SHA-256 (first 8 bytes) of:
- Image reference
- Command / Entrypoint
- Environment variables
- Ports
- Working directory
- User
- Privileged flag
- Restart policy

Hash computation is deterministic and done in Go before any Docker API calls.

> **Note:** the hash does **not** yet cover volumes, `cap_add`/`cap_drop`, or user labels, so
> changing only those fields does not currently trigger a recreate. Expanding the hash tracks with
> the field-support work in [ROADMAP.md](../ROADMAP.md#2-converge-engine--compose-coverage).

### Network & Volume Handling

- **Networks:** user-defined `compose.networks` are **not created yet** — containers currently run
  on the daemon's default bridge. This means Compose service-name DNS does not resolve between
  services in a stack. Named-network support is planned
  ([ROADMAP.md](../ROADMAP.md#2-converge-engine--compose-coverage)).
- **Volumes:** only **bind mounts** (`type: bind`, `source:target[:ro]`) are applied. Named volumes
  and tmpfs mounts are currently dropped. Anonymous volumes are per-container and recreated with the
  container.
- **Removal on REMOVED status:** managed containers are stopped and removed; orkestra does not
  create or remove networks/volumes on its own.

---

## Supported Compose Fields

### Validation vs. execution

Two different layers touch a compose file, and they do **not** agree on every field:

- **Validation** (`internal/shared/compose/validate.go`, used by the editor): an **unknown** field
  is a hard **error**; a small set of recognised-but-ignored fields (`deploy`, `profiles`, `links`,
  `external_links`, `scale`; top-level `configs`, `extensions`) produce a **warning**. Every other
  spec-valid field passes validation.
- **Execution** (`converge.go`): only the fields listed as ✅ below are actually translated onto the
  container. Fields that pass validation but aren't implemented are **silently ignored at deploy
  time** — this is the current behaviour, not the eventual goal.

> ⚠️ So a stack can validate cleanly and deploy "successfully" while fields like `networks`,
> named `volumes`, `depends_on`, or `healthcheck` have no effect. Full coverage (and failing loudly
> on unimplemented fields) is tracked in
> [ROADMAP.md](../ROADMAP.md#2-converge-engine--compose-coverage).

### `services.<name>` — currently applied

| Field | Support | Notes |
|---|---|---|
| `image` | ✅ | Pulled before create per `pull_policy` (anonymous — no private-registry auth) |
| `pull_policy` | ✅ | `always` / `never` / `build` / `missing` (default) |
| `command` | ✅ | |
| `entrypoint` | ✅ | |
| `environment` | ✅ | List and map form |
| `env_file` | ✅ | Resolved by the compose-go loader into `environment` |
| `ports` | ✅ | Short and long syntax; host IP + protocol |
| `restart` | ✅ | `no` / `always` / `unless-stopped` / `on-failure` (no `:max-retries` count) |
| `labels` | ✅ | Merged with orkestra system labels |
| `user` | ✅ | |
| `working_dir` | ✅ | |
| `privileged` | ✅ | |
| `cap_add` / `cap_drop` | ✅ | |
| `volumes` (bind mount) | ✅ | `source:target[:ro]`, host paths only |

### `services.<name>` — recognised but not yet applied (silently ignored)

`expose`, `volumes` (named/tmpfs), `networks`, `depends_on`, `healthcheck`, `hostname`,
`extra_hosts`, `dns`, `read_only`, `security_opt`, `sysctls`, `ulimits`, `mem_limit` /
`mem_reservation`, `cpus` / `cpu_shares`, `logging`, `stop_grace_period`, `init`, `tty` /
`stdin_open`, `devices`, `build`, `network_mode`. See
[ROADMAP.md](../ROADMAP.md#2-converge-engine--compose-coverage).

### `services.<name>` — flagged by the validator (warning, ignored)

`deploy` (Swarm), `profiles`, `links`, `external_links`, `scale`; top-level `configs`,
`extensions`. Native Compose `secrets`/`configs` are not used — use orkestra Secrets instead.
Any **unknown** field is a hard validation error.

---

## Drift Detection & Reporting

**Self-healing** is provided by the periodic reconcile: on every pass, any managed container that
has stopped or whose `spec-hash` no longer matches the desired spec is recreated. So a container
that is killed or drifts from its spec is brought back automatically on the next reconcile.

The `StatusReport`/`StackStatus` wire format carries per-stack running version, per-container state,
and `drift_detected` / `drift_description` fields, which the Master stores in `agent_state`. Rich
drift *reporting* to the UI (drift badges, human-readable drift descriptions) is only partially
wired — see [ROADMAP.md](../ROADMAP.md).

---

## Known Risk: Converge Engine Complexity

> **This is the highest-complexity package in the codebase.**
>
> `compose-go` parses the YAML into `types.Project` but provides **zero orchestration** logic.
> The Converge Engine effectively re-implements the core of `docker compose up --no-deps` (plus
> `down` and `recreate`). Edge cases to watch:
>
> - **Depends-on with healthcheck:** must poll container health via `docker inspect` before
>   starting dependent services. Use a polling loop with the `start_period` + `retries` budget.
> - **Volume ownership:** named volumes are not removed on `recreate` to preserve data.
> - **Concurrent ops:** services at the same dependency level can be started in parallel
>   (use `errgroup` with controlled concurrency).
> - **Image digest pinning:** pull the image and resolve to `image:tag@sha256:...` before
>   computing the spec-hash, so a `latest`-tagged image that changed triggers a recreate.
> - **Partial failure:** if one service fails to start, continue reconciling others and report
>   the error per-service; don't abort the whole stack.

The integration test suite for this package runs against a real Docker daemon (`dind` in CI).
