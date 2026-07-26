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
| `com.docker.compose.project` / `com.docker.compose.service` | for `docker compose ls` / tooling compatibility |

The **`spec-hash`** decides whether a container has to be recreated. It is a SHA-256 (first 8
bytes) over the identity-relevant part of the service spec:

- image reference
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
- **Volumes:** **bind mounts** are applied (`source:target[:ro]`, host paths). Named volumes and
  tmpfs mounts are not supported yet. Anonymous volumes are per-container and are recreated with
  the container.
- **Removal:** on `REMOVED`, managed containers are stopped and removed. orkestra does not create
  or delete networks and volumes on its own, so no data is destroyed behind the operator's back.

---

## Supported Compose Fields

Validation and execution share one definition. `internal/shared/compose` is used both by the
Master's `ValidateCompose` RPC (the diagnostics shown in the stack editor) and by the Agent, so a
stack that validates cleanly is a stack the Agent can actually run. Fields outside the supported
set are rejected with a clear message instead of being quietly dropped.

### `services.<name>` — supported

| Field | Notes |
|---|---|
| `image` | Resolved and pulled per `pull_policy` before the container spec is built |
| `pull_policy` | `always` / `never` / `build` / `missing` (default) |
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

Top level: `services` and `x-*` extension keys. A top-level `name` is ignored — the stack name is
owned by orkestra.

### `services.<name>` — not supported

Everything else, including: `networks`, named and tmpfs `volumes`, `depends_on`, `healthcheck`,
`expose`, `hostname`, `extra_hosts`, `dns`, `read_only`, `security_opt`, `sysctls`, `ulimits`,
`mem_limit` / `mem_reservation`, `cpus` / `cpu_shares`, `deploy`, `logging`, `stop_grace_period`,
`init`, `tty` / `stdin_open`, `devices`, `build`, `network_mode`, `container_name`, `profiles`,
`scale`, `links` / `external_links`. Top level: `configs`, `extensions`.

Compose-native `secrets` and `configs` are deliberately not used — orkestra Secrets are the
supported mechanism (see [05-secrets.md](05-secrets.md)). A field that is not part of the Compose
spec at all is a syntax error, reported with its line number.

---

## Drift Detection & Self-Healing

**Self-healing** comes from the periodic reconcile: on every pass, a managed container that has
stopped, disappeared, or whose `spec-hash` no longer matches the desired spec is recreated. A
container that someone killed or edited by hand is brought back on the next pass, at most 30
seconds later.

The `StatusReport` / `StackStatus` wire format carries the per-stack running version, per-container
state, and `drift_detected` / `drift_description`. Surfacing that in the UI (drift badges,
human-readable descriptions, container inventory) depends on persisting agent state, which is not
wired yet.

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
> - **Image digest pinning:** resolving a tag to `image@sha256:…` before hashing is what makes a
>   moved `latest` tag trigger a recreate.
> - **Field coverage vs. the hash:** any newly supported field must also enter the spec-hash, or
>   changing it will not recreate the container.

The integration tests for this package run against a real Docker daemon
(`make test-integration`, `dind` in CI).
