---
description: Start the full orkestra dev/test instance (Postgres, Master, Vite) using run-dev.sh
---

# run-dev skill

Use this skill when the user asks to start, run, or test the orkestra dev instance.

## IMPORTANT — ask about optional services first

**Before invoking the script**, if the user has not already specified which services to start
(or explicitly said "just the basics" / no extras), ask them:

```
AskUserQuestion(
  questions=[{
    question: "Which optional services should I also start alongside the base stack (Postgres + Master + Vite)?",
    header: "Optional services",
    multiSelect: true,
    options: [
      { label: "Keycloak", description: "Pre-configured OpenID Connect IdP for OIDC login testing (realm orkestra, port 8180)" },
      { label: "Mailpit",  description: "SMTP catcher with web UI — catches all outgoing emails (SMTP :2525, UI :8025)" },
      { label: "Agent",    description: "One orkestra agent with its own isolated Docker-in-Docker daemon, auto-enrolled and connected to the Master" }
    ]
  }]
)
```

Map the answers to flags:
- Keycloak selected → add `--keycloak` to the run-dev.sh invocation
- Mailpit selected  → add `--mailpit`
- Agent selected    → add `--agent` (one agent). For several, use `--agents N`
- Keycloak + Mailpit → `--all` (then append `--agent`/`--agents N` if also selected)
- Neither           → no extra flags (base stack only)

If the user already mentioned specific services ("start with Keycloak", "run dev with mailpit",
"start with 2 agents", "start everything", etc.) skip the question and map directly to the
appropriate flags (`--agents 2` for a specific count).

## What this skill does

Runs `./run-dev.sh [flags]` from the repo root. The script:

1. Creates the `orkestra-dev-pg` Docker container (Postgres 16) fresh — previous run's DB is gone.
2. If `--mailpit`/`--all`: starts a fresh `orkestra-dev-mailpit` container (Mailpit SMTP catcher).
3. If `--keycloak`/`--all`: starts a fresh `orkestra-dev-keycloak` container (Keycloak IdP) and
   waits for the `orkestra` realm to become reachable (~30–40 s on first boot).
4. Builds the dev binary (`make build-dev`) — no web embed, Master proxies to Vite on :5173.
5. Starts `bin/orkestra-master` with `ORKESTRA_DATABASE_URL` and `ORKESTRA_MASTER_KEY` set.
6. Auto-configures the dev instance via the master API:
   - Creates dev admin `admin@orkestra.local` / `orkestra-dev` via the first-run setup token.
   - If `--mailpit`: applies SMTP settings (host localhost, port 2525, from orkestra@dev.local).
   - If `--keycloak`: applies OIDC settings (issuer http://localhost:8180/realms/orkestra).
7. If `--agent`/`--agents N`: for each agent, starts a privileged `orkestra-dev-agent-<i>-dind`
   container (isolated dockerd), mints a bootstrap token via the admin API, enrolls
   `bin/orkestra-agent` into `/tmp/orkestra-dev-agent-<i>`, and runs `orkestra-agent serve`
   as a host process pointed at that DinD daemon (`DOCKER_HOST`). Test stacks run ONLY inside
   each agent's own DinD daemon — never on the host's Docker. Agent logs: `/tmp/orkestra-agent-<i>.log`.
8. Starts the Vite dev server (`cd web && npm run dev`).
9. On exit (Ctrl+C or process death): kills Master + Vite + all agent processes, then **removes**
   all dev containers including the DinD daemons (`docker rm -f`) and agent data dirs. Each new
   run starts with a completely fresh database and freshly enrolled agents.

Migrations run automatically on Master startup (goose, embedded SQL).

## Dev admin credentials

Created automatically by `run-dev.sh` on every startup (fresh DB each run):

| Field | Value |
|---|---|
| Email | `admin@orkestra.local` |
| Password | `orkestra-dev` |

Override via `.env`: `ORKESTRA_DEV_ADMIN_EMAIL` / `ORKESTRA_DEV_ADMIN_PASSWORD`.

## Environment

| Variable | Default |
|---|---|
| `ORKESTRA_DATABASE_URL` | `postgres://orkestra:orkestra@localhost:5432/orkestra?sslmode=disable` |
| `ORKESTRA_MASTER_KEY` | `0000000000000000000000000000000000000000000000000000000000000001` (dev only) |
| `ORKESTRA_DEV_AGENTS` | `0` (number of agents; same as `--agents N`) |
| `ORKESTRA_DEV_DIND_BASE_PORT` | `23751` (agent `i` daemon → host port `23751+i-1`) |
| `ORKESTRA_DEV_AGENT_METRICS_BASE_PORT` | `9091` (agent `i` metrics → `9091+i-1`) |
| `ORKESTRA_DEV_DIND_IMAGE` | `docker:dind` |

## Endpoints after startup

| Service | URL |
|---|---|
| UI + API | http://localhost:8080 |
| Vite HMR | http://localhost:5173 |
| Metrics | http://localhost:9090/metrics |
| Health | http://localhost:8080/healthz |

### Optional services

| Service | URL | Flags / Env |
|---|---|---|
| Mailpit web UI | http://localhost:8025 | `--mailpit` / `ORKESTRA_DEV_MAILPIT=1` |
| Mailpit SMTP | localhost:2525 | (same as above) |
| Keycloak admin | http://localhost:8180 | `--keycloak` / `ORKESTRA_DEV_KEYCLOAK=1` |
| Keycloak OIDC discovery | http://localhost:8180/realms/orkestra/.well-known/openid-configuration | (same) |
| Agent `i` DinD daemon | `tcp://localhost:2375<i>` (23751, 23752, …) | `--agent` / `--agents N` / `ORKESTRA_DEV_AGENTS=N` |

### Inspecting agents

- Agents show up on the **Servers** page as `dev-agent-1`, `dev-agent-2`, … once connected.
- Each agent's containers live only in its isolated daemon:
  `docker -H tcp://localhost:23751 ps` (agent 1), `…23752` (agent 2), etc.
- Deploy a stack to a `dev-agent-*` server in the UI to exercise reconciliation.

## Logs

- Master: `/tmp/orkestra-master.log`
- Vite:   `/tmp/orkestra-vite.log`
- Agents: `/tmp/orkestra-agent-<i>.log` (enrollment + serve)

## How to invoke

```bash
# (with optional flags from the question above, e.g. --all)
tmux new-session -d -s orkestra-dev -x 220 -y 50
tmux send-keys -t orkestra-dev "cd /Users/tobiashecker/Documents/repos/orkestra && ./run-dev.sh [FLAGS]" Enter

# Wait for the master + auto-config to complete (up to 90 s — longer when Keycloak starts cold)
for i in $(seq 1 90); do
  curl -sf http://localhost:8080/healthz > /dev/null 2>&1 && break
  sleep 1
done
sleep 3  # allow auto_configure() to finish
```

After startup, check `tmux capture-pane -t orkestra-dev -p` for the ready banner — it shows the
dev admin credentials and which services were auto-configured.

## Settings (auto-applied — reference only)

### Mailpit → Settings → Email (applied automatically when `--mailpit`)
| Field | Value |
|---|---|
| SMTP host | `localhost` |
| SMTP port | `2525` |
| STARTTLS | disabled |
| From address | `orkestra@dev.local` |
| Public URL | `http://localhost:8080` |

### Keycloak → Settings → OIDC (applied automatically when `--keycloak`)
| Field | Value |
|---|---|
| Issuer URL | `http://localhost:8180/realms/orkestra` |
| Client ID | `orkestra` |
| Client secret | `orkestra-dev-secret` |
| Groups claim | `groups` |

- Test user: `testuser@example.com` / password `test`
- **Pre-create `testuser@example.com` as a local user in orkestra before logging in via SSO.**
- Optional role mapping: group `orkestra-admins` → role `admin`
- Keycloak admin console: http://localhost:8180 — login `admin` / `admin`

## Stopping

```bash
# Ctrl+C in the tmux session, or:
tmux send-keys -t orkestra-dev C-c
sleep 2
tmux kill-session -t orkestra-dev 2>/dev/null || true
# run-dev.sh cleanup() removes ALL dev containers on exit (Postgres, Mailpit, Keycloak,
# and every agent DinD daemon) plus agent data dirs, and kills the agent processes.
# No manual docker rm needed.
```
