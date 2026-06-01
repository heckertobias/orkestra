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
4. A secret referenced by an active stack version is updated.
5. An Agent reconnects after being offline (Master always re-pushes full desired state on reconnect).
6. A periodic **full resync** (configurable, default 5 minutes) to detect external drift.

The message contains the **full** desired state for that server (not a diff) — this makes it
safe to re-send on reconnect without any missed-delta issues.

---

## Agent-Side: Reconcile Loop

The reconcile loop runs:
- Immediately on receiving `ApplyDesiredState` from the Master.
- Periodically every 30 seconds (resync against Docker actual state, catches external drift).

### Algorithm per Stack

```
function reconcile(desiredStack StackDesiredState):

  if desiredStack.status == REMOVED:
    remove all containers with label orkestra.stack=<stack_id>
    remove stack-specific networks and volumes (if not shared)
    return

  project = compose_go.Load(
    compose_yaml  = desiredStack.compose_yaml,
    env_overrides = desiredStack.env_vars,
    secrets       = materialize(desiredStack.secrets),  // → tmpfs files / env
  )
  // project: types.Project (parsed service graph, no orchestration)

  desired  = derive_desired_containers(project)
  actual   = docker.ContainerList(filter: label=orkestra.stack=<stack_id>)

  diff = compute_diff(desired, actual)
  //  diff entries: { action: create|recreate|keep|remove, service, spec }

  if desiredStack.status == STOPPED:
    // stop all running containers but don't remove them
    for c in actual where c.state == "running":
      docker.ContainerStop(c.id)
    return

  // Apply diff in dependency order (respects depends_on graph)
  order = topological_sort(project.services, by: depends_on)

  for service in order:
    entry = diff[service.name]
    switch entry.action:
      case remove:
        docker.ContainerStop(entry.container_id)
        docker.ContainerRemove(entry.container_id)

      case create:
        ensure_networks(service)
        ensure_volumes(service)
        docker.ImagePull(service.image) if digest_changed
        id = docker.ContainerCreate(spec_from_service(service, secrets))
        docker.ContainerStart(id)
        if service.healthcheck:
          wait_healthy(id, timeout=service.healthcheck.start_period)

      case recreate:
        docker.ContainerStop(entry.container_id)
        docker.ContainerRemove(entry.container_id)
        // then create (same as above)

      case keep:
        // nothing — container matches desired spec

  report StatusReport to Master
```

### Container Identity & Idempotency

Every orkestra-managed container carries these labels:

| Label | Value |
|---|---|
| `orkestra.managed` | `true` |
| `orkestra.stack` | `<stack_id>` |
| `orkestra.version` | `<stack_version_id>` |
| `orkestra.service` | `<compose_service_name>` |
| `orkestra.spec-hash` | SHA-256 of the normalized service spec |

The **`spec-hash`** determines whether a `recreate` is needed. It is computed as a SHA-256 of:
- Image reference (resolved to digest if possible)
- Environment variables (sorted keys)
- Port bindings (sorted)
- Volume mounts (sorted)
- Command / Entrypoint
- Labels (only orkestra.* excluded from hash)
- Network aliases
- Resource limits (memory, CPU)
- Restart policy

Hash computation is deterministic and done in Go before any Docker API calls.

### Network & Volume Handling

- **Networks:** Stack-specific networks (defined in `compose.networks`) are created as
  `orkestra_<stack_id>_<network_name>`. Shared (external) networks are not touched.
- **Volumes:** Named volumes follow `orkestra_<stack_id>_<volume_name>`. Anonymous volumes are
  per-container and recreated with the container.
- **Removal on REMOVED status:** Only non-external networks/volumes are removed. Removal is
  attempted after all containers are stopped; errors are logged but don't block reporting.

---

## Supported Compose Fields (MVP Matrix)

The Converge Engine supports this subset of the Compose Specification in the MVP. Using an
unsupported field causes the deploy to **fail with an explicit error** (never silently ignored).

### `services.<name>`

| Field | Support | Notes |
|---|---|---|
| `image` | ✅ Full | Image pull before create |
| `build` | ⚠️ Partial | Build context on local Docker daemon; `build.args`, `build.target`. No BuildKit cache-from/to. |
| `command` | ✅ Full | |
| `entrypoint` | ✅ Full | |
| `environment` | ✅ Full | List and map form |
| `env_file` | ✅ Full | Relative to stack root (stored alongside compose_yaml) |
| `ports` | ✅ Full | Short and long syntax; host IP binding |
| `expose` | ✅ Full | |
| `volumes` (named) | ✅ Full | `source:target:mode` |
| `volumes` (bind mount) | ✅ Full | Absolute paths on host |
| `volumes` (tmpfs) | ✅ Full | |
| `networks` | ✅ Full | Custom networks, aliases |
| `depends_on` | ✅ Full | `condition: service_started` and `service_healthy` |
| `restart` | ✅ Full | no / always / unless-stopped / on-failure[:n] |
| `healthcheck` | ✅ Full | test, interval, timeout, retries, start_period |
| `labels` | ✅ Full | Merged with orkestra system labels |
| `user` | ✅ Full | |
| `working_dir` | ✅ Full | |
| `hostname` | ✅ Full | |
| `extra_hosts` | ✅ Full | |
| `dns` | ✅ Full | |
| `cap_add` / `cap_drop` | ✅ Full | |
| `privileged` | ✅ Full | |
| `read_only` | ✅ Full | |
| `security_opt` | ✅ Full | |
| `sysctls` | ✅ Full | |
| `ulimits` | ✅ Full | |
| `mem_limit` / `mem_reservation` | ✅ Full | (top-level shortcuts) |
| `cpus` / `cpu_shares` | ✅ Full | |
| `logging` | ✅ Full | driver + options |
| `stop_grace_period` | ✅ Full | |
| `init` | ✅ Full | |
| `tty` / `stdin_open` | ✅ Full | |
| `profiles` | ❌ Not supported | Error: use separate stacks |
| `extends` | ❌ Not supported | Pre-merge compose files before submitting |
| `deploy` (swarm) | ❌ Not supported | Not Swarm |
| `configs` | ❌ Not supported | Use orkestra Secrets instead |
| `secrets` (compose native) | ❌ Not supported | Use orkestra Secrets instead |
| `scale` | ❌ Not supported (M6+) | Single replica per service in MVP |
| `links` | ❌ Not supported | Use networks |
| `external_links` | ❌ Not supported | |
| `volumes_from` | ❌ Not supported | |
| `network_mode: host/none/container` | ⚠️ Partial | `host` and `none` supported; `container:X` not |

### `networks.<name>` / `volumes.<name>`

| Field | Support |
|---|---|
| `driver` | ✅ Full |
| `driver_opts` | ✅ Full |
| `external` | ✅ Full |
| `ipam` | ✅ Full |
| `attachable` | ✅ Full |

---

## Drift Detection & Reporting

After each reconcile pass, the Agent sends a `StatusReport` containing:
- For each stack: running version, per-container state, and a `drift_detected` boolean.
- `drift_description`: human-readable summary of what was drifted (e.g. "container nginx_web_1
  found stopped, expected running").

The Master stores this in `agent_state` and makes it visible in the UI (drift badge on server
and stack cards). Drift automatically triggers a reconcile (self-healing).

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
