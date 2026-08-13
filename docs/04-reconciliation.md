# orkestra — Desired State & Reconciliation

## Model

The **Desired State** of a server is the union of all active `assignments` for that server: each
assignment binds a `stack_version` (its `compose_yaml`, plus the assignment's `env_values` and the
version's `secret_refs`) to a server with a `desired_status` (running | stopped | removed).

The Master computes and distributes this state; the Agent **reconciles** the actual Docker state
toward it.

---

## Master-Side: Triggering Reconciliation

The Master pushes `ApplyDesiredState` to an Agent whenever:

1. A stack is assigned, re-assigned, or rolled back (`PushNow` right after the mutation).
2. An assignment's `desired_status` changes.
3. An assignment is removed.
4. An Agent (re)connects — the current desired state is pushed immediately.
5. On the reconciler's ticker: every 15 s the full desired state goes out to every connected agent.

The message always contains the **full** desired state for that server, never a diff. That is what
makes re-sending safe: a reconnecting agent cannot miss a delta, because there are no deltas.

---

## Agent-Side: Reconcile Loop

The reconcile loop runs:
- Immediately on receiving `ApplyDesiredState` from the Master.
- Periodically every 30 seconds against the last received state — this is the resync that catches
  drift caused by anything happening outside orkestra.

If a new desired state arrives while one is queued, the newer state wins.

### Algorithm per Stack

```
function reconcile(desiredStack StackDesiredState):

  if desiredStack.status == REMOVED:
    stop + remove all containers labelled orkestra.stack-id=<stack_id>
    return

  if desiredStack.status == STOPPED:
    stop (but keep) all containers labelled orkestra.stack-id=<stack_id>
    return

  project = compose_go.Load(
    compose_yaml = desiredStack.compose_yaml,
    env          = desiredStack.env_vars,   // the assignment's values
  )
  // project: types.Project — the parsed service graph; compose-go does no orchestration

  actual = docker.ContainerList(filter: label orkestra.stack-id=<stack_id>)

  for service in sorted(project.services):
    ensure_image(service.image, service.pull_policy)   // resolve/pull before deciding
    hash = spec_hash(service)
    cur  = actual[service.name]

    if cur exists AND cur.spec-hash == hash AND cur.state == "running":
      keep                                   // up to date, nothing to do
    else:
      if cur exists: stop + remove cur       // drifted, stopped, or spec changed → recreate
      id = docker.ContainerCreate(spec_from_service(service))
      docker.ContainerStart(id)

  // Any managed container whose service is no longer in the project is removed.
  for orphan in actual not in project.services:
    stop + remove orphan

  report StatusReport to Master
```

**Removed stacks.** The per-stack algorithm above only reconciles stacks that are *present* in the
push. Because the push is the **full** desired state, a stack that has been unassigned or deleted
simply stops appearing. After processing the push, the agent lists every managed stack on the host
(by the `orkestra.stack-id` label) and stops + removes any whose ID is no longer in the desired
state — the same effect as an explicit `REMOVED` status. This is what makes unassign and delete
converge without leaving zombie containers behind. An empty push therefore removes everything the
agent manages, which is correct: it means the server has no assignments.

**Interpolation.** `${VAR}` references in the compose file are resolved from the desired state's
`env_vars` (the assignment's values) and the stack's own `env_file` / `environment` declarations.
The result is deterministic per assignment and does not depend on what happens to be set in the
agent process's environment.

**Partial failure.** A service that fails to build, pull, create, or start is reported per service;
the remaining services of the stack are still reconciled. One broken service does not take the rest
of the stack with it.

### Container Identity & Idempotency

Every orkestra-managed container carries these labels:

| Label | Value |
|---|---|
| `orkestra.managed` | `true` |
| `orkestra.stack-id` | `<stack_id>` |
| `orkestra.service` | `<compose_service_name>` |
| `orkestra.spec-hash` | SHA-256 (truncated) of the normalised service spec |
| `orkestra.stack-version` | `<stack_version_id>` the container was created from |
| `com.docker.compose.project` / `com.docker.compose.service` | for `docker compose ls` / tooling compatibility |

`orkestra.stack-version` is not part of the identity: it records which version *created* the
container so the Agent can report what is actually running rather than what was last pushed. It
does not enter the `spec-hash`, so a new version whose service specs are byte-identical does not
recreate anything — and the label then still names the older version, which is the truthful answer
about the container in front of it.

The **`spec-hash`** decides whether a container has to be recreated. It is a SHA-256 (first 8
bytes) over the identity-relevant part of the service spec:

- image reference **and the resolved image ID** — the tag is pulled per `pull_policy` and inspected
  *before* the recreate decision, so a moved tag (e.g. a repointed `latest`) changes the hash and
  recreates, and a failed pull leaves the running container untouched
- command / entrypoint
- environment variables
- ports
- working directory
- user
- privileged flag
- restart policy

Hash computation is deterministic and happens in Go before any Docker API call. Every field that
orkestra actually applies to a container is meant to feed the hash — otherwise changing that field
would silently leave the old container running. The hash currently omits volumes, capabilities, and
user labels; extending it goes hand in hand with widening field support.

### Networks & Volumes

- **Networks:** containers run on the daemon's default bridge. User-defined `networks` (and thus
  Compose service-name DNS between services of a stack) are not supported yet.
- **Volumes:** **bind mounts** are applied (`source:target[:ro]`, host paths). Every other mount
  type — named volumes, tmpfs, and anonymous volumes — is **rejected** with a clear error, at the
  editor/API and again in the agent, rather than being silently dropped: a dropped named volume
  would send a database's writes into the container layer and destroy them on the next recreate.
  Named and tmpfs volume support is tracked separately.
- **Removal:** on `REMOVED`, managed containers are stopped and removed. orkestra does not create
  or delete networks and volumes on its own, so no data is destroyed behind the operator's back.

---

## Supported Compose Fields

Validation and execution share one definition. `internal/shared/compose` is used both by the
Master's `ValidateCompose` RPC (the diagnostics shown in the stack editor) and by the Agent, so a
stack that validates cleanly is a stack the Agent can actually run. Nothing outside the supported
set is ever quietly dropped: an unsupported construct is either an **error** (the create/update is
refused) when applying it anyway would do the wrong thing, or a **warning** (accepted, but the
operator is told the field has no effect).

### `services.<name>` — supported

| Field | Notes |
|---|---|
| `image` | Resolved and pulled per `pull_policy` before the container spec is built |
| `pull_policy` | `always` / `never` / `build` / `missing` (default) / `if_not_present`. The time-based policies (`refresh`, `daily`, `weekly`, `every_*`) are rejected — they need pull-history state that does not exist yet |
| `command` | |
| `entrypoint` | |
| `environment` | List and map form |
| `env_file` | Resolved by the compose-go loader into `environment` |
| `ports` | Short and long syntax; host IP + protocol |
| `restart` | `no` / `always` / `unless-stopped` / `on-failure` (incl. `on-failure:N`) |
| `labels` | Merged with the orkestra system labels |
| `user` | |
| `working_dir` | |
| `privileged` | |
| `cap_add` / `cap_drop` | |
| `volumes` | Bind mounts only: `source:target[:ro]` with a host path |
| `extends` | In-file only (`extends: name` or `extends: {service: ...}`); resolved by compose-go |

Top level: `services` and `x-*` extension keys. A top-level `name` is ignored — the stack name is
owned by orkestra.

### `services.<name>` — rejected (error)

Applying these anyway would do the wrong thing, so they are refused at create/update:

- named / tmpfs / anonymous `volumes` (only bind mounts are applied)
- `profiles` — a profiled service would be loaded and then removed as an orphan
- `configs` — the config is never delivered to the container
- `extends: {file: ...}` — the agent loads a single file, so a file extends fails at load time and
  stops the whole stack from reconciling
- top level: `include` (same load-time failure as `extends: file:`)

### `services.<name>` — ignored (warning)

Accepted so the stack still runs, but the field has no effect and the editor says so:
`container_name`, `healthcheck`, `logging`, `secrets`, `deploy`, `scale`, `links` /
`external_links`. Top level: `configs`, `secrets`, `extensions`.

Compose-native `secrets` and `configs` are deliberately not used — orkestra Secrets are the
supported mechanism (see [05-secrets.md](05-secrets.md)). Everything else in the Compose spec that
orkestra does not apply (`networks`, `depends_on`, `expose`, `dns`, resource limits, `build`,
`network_mode`, …) is not supported yet. A field that is not part of the Compose spec at all is a
syntax error, reported with its line number.

---

## Drift Detection & Self-Healing

**Self-healing** comes from the periodic reconcile: on every pass, a managed container that has
stopped, disappeared, or whose `spec-hash` no longer matches the desired spec is recreated. A
container that someone killed or edited by hand is brought back on the next pass, at most 30
seconds later.

### Reported State

Every 30 seconds — and once immediately after `Hello`, so a fresh connection does not leave the UI
blank for half a minute — the Agent reports what is on the host:

- it lists the containers labelled `orkestra.managed=true` and groups them by `orkestra.stack-id`,
- it inspects each one for the restart count and start time, which a container list does not carry,
- it fills `running_version` from `orkestra.stack-version` when every container of the stack agrees
  on one — a half-finished rollout has no single running version and reports none,
- and it attaches the outcome of the last reconcile: `error` per stack plus `service_errors` per
  service, so a stack that failed before any container existed still reports why.

The Master stores the report verbatim in `agent_state` and serves it to the UI. `drift_detected` /
`drift_description` are part of the same message but are not populated yet.

---

## Known Risk: Converge Engine Complexity

> **This is the highest-complexity package in the codebase.**
>
> `compose-go` parses YAML into `types.Project` but provides **zero orchestration** logic. The
> Converge Engine effectively re-implements the core of `docker compose up` (plus `down` and
> `recreate`). The edge cases that decide whether it behaves like Compose:
>
> - **Ordering and health gating:** `depends_on` with `condition: service_healthy` requires polling
>   container health before starting dependents, with the `start_period` + `retries` budget as the
>   timeout.
> - **Volume ownership:** named volumes must survive a recreate — recreating a container must never
>   drop its data.
> - **Concurrency:** services at the same dependency level can start in parallel (`errgroup` with
>   bounded concurrency).
> - **Field coverage vs. the hash:** any newly supported field must also enter the spec-hash, or
>   changing it will not recreate the container.

The integration tests for this package run against a real Docker daemon
(`make test-integration`, `dind` in CI).
