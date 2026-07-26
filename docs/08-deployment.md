# orkestra — Observability & Deployment

## Observability

### Logging

Both the Master and Agent use **`log/slog`** (Go stdlib structured logging) with the text handler,
writing to stderr — under systemd that lands in the journal (`SyslogIdentifier=orkestra-master` /
`orkestra-agent`), under Docker in the container log.

Log level is configurable via the `--log-level` flag or the `ORKESTRA_LOG_LEVEL` env var
(`debug | info | warn | error`; default: `info`).

Recurring structured fields:
- `agent_id` — on agent-related lines in the Master.
- `stack_id`, `service` — on reconcile and converge lines in the Agent.
- `err` — on every error line.

### Metrics (Prometheus)

`/metrics` endpoint on `:9090` (Master). Agents expose `:9091` locally, but you don't scrape it
directly — see **Federated agent metrics** below.

**Master metrics:**

| Metric | Type | Description |
|---|---|---|
| `orkestra_agents_connected_total` | Gauge | Currently connected Agents |
| `orkestra_agents_offline_total` | Gauge | Agents marked offline |
| `orkestra_deploy_duration_seconds` | Histogram | Time from deploy trigger to reconcile report |
| `orkestra_deploy_total` | Counter | Deployments by `status` (success/error) |
| `orkestra_reconcile_push_total` | Counter | ApplyDesiredState messages sent |
| `orkestra_api_requests_total` | Counter | UI API requests by `method`, `status` |
| `orkestra_api_duration_seconds` | Histogram | UI API latency |
| `orkestra_secret_resolves_total` | Counter | Secret provider calls by `provider`, `status` |

**Agent metrics:**

| Metric | Type | Description |
|---|---|---|
| `orkestra_agent_containers_running` | Gauge | Currently running managed containers |
| `orkestra_agent_containers_drift` | Gauge | Containers in drift state |
| `orkestra_agent_reconcile_duration_seconds` | Histogram | Per-stack reconcile duration |
| `orkestra_agent_reconcile_errors_total` | Counter | Reconcile errors by `stack_id` |
| `orkestra_agent_docker_api_duration_seconds` | Histogram | Docker SDK call latency by `operation` |
| `orkestra_agent_stream_reconnects_total` | Counter | Master stream reconnects |

#### Federated agent metrics

Agents connect **outbound** only, so their `:9091` endpoint is not reachable from a central
Prometheus without opening inbound ports per host. Instead, the Master **federates** them: on
request it asks the target agent for its metrics over the existing mTLS stream and returns them.

- **Endpoint:** `GET /api/agents/{agent_id}/metrics` on the Master's UI/API port (behind `443`).
- **Auth:** same as the rest of the API — a session cookie or a **Bearer API key**. Create an API
  key for a scrape user and configure Prometheus with `authorization.credentials`.
- Returns Prometheus text exposition format; `503` if the agent is offline.

Example Prometheus scrape config (one target per agent, or via service discovery):

```yaml
scrape_configs:
  - job_name: orkestra-agents
    scheme: https
    authorization:
      credentials: "<orkestra-api-key>"
    metrics_path: /api/agents/<agent_id>/metrics
    static_configs:
      - targets: ["orkestra.example.com"]   # the Master, not the agent
```

No inbound port is required on the agent hosts; the only agent-facing hole in the firewall is the
Master's agent port `4440`.

### Health Endpoints (Master)

| Endpoint | Description |
|---|---|
| `GET /healthz` | Liveness: always 200 if process is running |
| `GET /readyz` | Readiness: 200 if the database responds to a ping, else 503 |

---

## Install via apt / dnf (packages)

The recommended way to install on bare metal is the signed package repository — you get
`apt install` / `dnf install` and, crucially, **`apt upgrade` / `dnf upgrade`** for updates instead
of re-running a script. Packages are published for both `orkestra-master` and `orkestra-agent`
(amd64 + arm64) and drop in the systemd unit, a default `/etc/orkestra/<tool>/env`, and a system
user where needed.

### Debian / Ubuntu (apt)

```bash
curl -fsSL https://heckertobias.github.io/orkestra/orkestra.gpg \
  | sudo tee /usr/share/keyrings/orkestra.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/orkestra.gpg] https://heckertobias.github.io/orkestra/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/orkestra.list
sudo apt update
sudo apt install orkestra-agent        # or: orkestra-master
```

### Fedora / RHEL / openSUSE (dnf)

```bash
sudo dnf config-manager --add-repo https://heckertobias.github.io/orkestra/rpm/orkestra.repo
sudo dnf install orkestra-agent        # or: orkestra-master
```

### After install

The package installs and `systemctl enable`s the service but does **not** start it — one config
step remains:

- **Agent:** enroll once, then start.
  ```bash
  sudo orkestra-agent enroll --master https://<master>:4440 --bootstrap-token <token> --name web-01
  sudo systemctl enable --now orkestra-agent
  ```
  Get the token from the Master UI → **Servers → Add Server**.
- **Master:** set `ORKESTRA_DATABASE_URL` and create the KEK in `/etc/orkestra/master/env`
  (see the comments in that file), then `sudo systemctl enable --now orkestra-master`. For most
  setups, running the Master via Docker/Compose (below) is simpler because it bundles Postgres.

### Updating

```bash
sudo apt update && sudo apt upgrade     # Debian/Ubuntu
sudo dnf upgrade                        # Fedora/RHEL
```

> The older `install-agent.sh` (below) remains as a fallback for hosts without the package repo, but
> it does not self-update — prefer the package manager where possible.

---

## Deployment: Master

### Option A — Docker Compose (Recommended for self-hosting)

`deploy/docker/compose.yaml`:

```yaml
secrets:
  orkestra_master_key:
    file: ./secrets/master_key  # hex-encoded 32-byte KEK — chmod 600, NOT in .env

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: orkestra
      POSTGRES_USER: orkestra
      POSTGRES_PASSWORD: "${POSTGRES_PASSWORD}"
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U orkestra"]
      interval: 10s
      timeout: 5s
      retries: 5

  master:
    image: ghcr.io/heckertobias/orkestra-master:latest
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "4440:4440"   # Agent gRPC (mTLS) — 4440 = orchestra concert pitch A440
      - "8080:8080"   # Web UI + API (put a TLS-terminating reverse proxy in front)
      - "9090:9090"   # Prometheus metrics — restrict at the firewall, never expose publicly
    secrets:
      - orkestra_master_key
    environment:
      # KEK is read from the secret mount — never set ORKESTRA_MASTER_KEY here
      ORKESTRA_MASTER_KEY_FILE: /run/secrets/orkestra_master_key
      ORKESTRA_DATABASE_URL: "postgres://orkestra:${POSTGRES_PASSWORD}@postgres:5432/orkestra?sslmode=disable"
      ORKESTRA_AGENT_ADDR: "0.0.0.0:4440"
      ORKESTRA_UI_ADDR: "0.0.0.0:8080"
      ORKESTRA_METRICS_ADDR: "0.0.0.0:9090"
      ORKESTRA_LOG_LEVEL: info
      # Browser-facing base URL — required for OIDC, email links, and the setup link.
      # ORKESTRA_PUBLIC_URL: https://orkestra.example.com
      # Session/OIDC cookies carry the Secure attribute by default (HTTPS only).
      # Set to false ONLY for direct plain-HTTP access (local dev).
      # ORKESTRA_SECURE_COOKIES: "true"
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

volumes:
  postgres-data:
```

The Master serves the UI port over plain HTTP; TLS for the browser is terminated by a reverse proxy
in front of `:8080` (see *Ingress & Networking* below).

The KEK lives in `secrets/master_key` — a `chmod 600` file that Docker mounts as tmpfs under
`/run/secrets/orkestra_master_key`. It never appears in `environment:` or `.env`. The DB password
(in `.env`) and the KEK (as a secret file) are in **separate trust domains**: a compromised `.env`
does not reveal the KEK, and a stolen DB dump cannot be decrypted without it.

First run:
```bash
# DB password — safe in .env (DB credentials only)
export POSTGRES_PASSWORD=$(openssl rand -hex 24)
echo "POSTGRES_PASSWORD=$POSTGRES_PASSWORD" >> .env

# KEK — lives in a SEPARATE file, never in .env
mkdir -p secrets
openssl rand -hex 32 > secrets/master_key
chmod 600 secrets/master_key
# Back this file up to a password manager or HSM — losing it means losing all encrypted data.

docker compose up -d
# Open the setup URL printed in the logs
docker compose logs master | grep "setup"
```

### Option B — Systemd

`deploy/systemd/orkestra-master.service`:

```ini
[Unit]
Description=orkestra Master — Docker Orchestration Control Plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=orkestra
Group=orkestra
ExecStart=/usr/bin/orkestra-master
EnvironmentFile=-/etc/orkestra/master/env
Restart=on-failure
RestartSec=5s

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/orkestra
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictRealtime=true

[Install]
WantedBy=multi-user.target
```

`/etc/orkestra/master/env` (DB credentials only — no KEK here):
```
ORKESTRA_DATABASE_URL=postgres://orkestra:<password>@localhost:5432/orkestra?sslmode=disable
ORKESTRA_AGENT_ADDR=0.0.0.0:4440
ORKESTRA_UI_ADDR=0.0.0.0:8080
ORKESTRA_METRICS_ADDR=127.0.0.1:9090
ORKESTRA_MASTER_KEY_FILE=/etc/orkestra/master/master.key
ORKESTRA_PUBLIC_URL=https://orkestra.example.com
# Secure cookies are on by default; set false only for direct plain-HTTP access (local dev).
# ORKESTRA_SECURE_COOKIES=true
```

`/etc/orkestra/master/master.key` (KEK — stored separately, **not** in the env file):
```bash
# Create once:
openssl rand -hex 32 > /etc/orkestra/master/master.key
chmod 600 /etc/orkestra/master/master.key
chown root:root /etc/orkestra/master/master.key
# Back up to a password manager or HSM — separate from the DB backup.
```

> Optionally, use systemd `LoadCredential=master.key:/etc/orkestra/master/master.key` and
> set `ORKESTRA_MASTER_KEY_FILE=%d/master.key` for in-memory credential passing.

---

## Ingress & Networking

The Master exposes two externally-relevant endpoints on **separate ports** (plus internal metrics):

| Port | Purpose | Exposure |
|---|---|---|
| `8080` | Web UI + API (browser, Connect protocol) | Public — typically behind a reverse proxy terminating TLS on `443` |
| `4440` | Agent gRPC (mTLS, HTTP/2) | Open to agents; **TLS passthrough only** — must NOT be terminated by a proxy |
| `9090` | Prometheus metrics (Master) | Internal only — bind to loopback / firewall off |

> Why `4440`? It's the orchestra concert pitch **A = 440 Hz** — distinctive and unlikely to
> collide with common services (unlike the crowded `8443`).

### Behind a domain / reverse proxy

Typical setup: the UI lives behind a public domain (e.g. `https://orkestra.example.com`) while
the agent channel keeps its own port.

- **UI (`8080`):** a reverse proxy (nginx/Traefik/Caddy) terminates Let's Encrypt on `443`
  and forwards to `:8080`. Standard HTTPS. Leave **`ORKESTRA_SECURE_COOKIES`** at its default
  (`true`): the browser↔proxy leg is HTTPS, so session/OIDC cookies are set with the `Secure`
  attribute even though the proxy→Master hop is plain HTTP. Only set it `false` when the UI is
  reached directly over plain HTTP (local dev) — otherwise the browser drops the cookies and
  login fails.
- **Public URL:** the browser-facing base URL (e.g. `https://orkestra.example.com`) the Master uses
  to build the OIDC `redirect_uri`, the first-run setup link, and password-reset/invite email links.
  Behind TLS this is **required for OIDC** — the registered redirect URI must match
  `<public URL>/auth/oidc/callback`; without it the Master falls back to the bind address over
  `http://`, which mismatches the IdP registration. Set it either way, in order of precedence:
    1. **UI** — *Settings → General → Public URL* (admin, stored in `server_config`). Applied live:
       a change re-initialises the OIDC provider without a Master restart.
    2. **`ORKESTRA_PUBLIC_URL`** — the startup default when no UI value is set (declarative/GitOps).
    3. **Fallback** — for email links the proxy's `X-Forwarded-Proto`/`X-Forwarded-Host` headers;
       otherwise the bind address with the scheme from `ORKESTRA_SECURE_COOKIES` (`https` by
       default). The first-run setup link runs before any admin exists, so it always uses this
       env/fallback layer.
- **Agent mTLS (`4440`):** must be reachable end-to-end. **Do not terminate TLS at the proxy** —
  agents pin the Master's *internal CA*, so a public-cert proxy would break the handshake.
  Either forward the port directly, or use a **TCP/SNI passthrough** (nginx `stream` with
  `ssl_preread`, or Traefik TCP with SNI) that does not decrypt. Agents enroll with
  `--master https://orkestra.example.com:4440`.
- **Required:** add the public hostname to the Master's agent-cert SANs via
  **`ORKESTRA_AGENT_TLS_SANS`** (comma-separated hostnames/IPs). Loopback names
  (`localhost`, `127.0.0.1`, `::1`) are always included automatically. Without this, agents
  dialing the public name fail certificate verification.

```
# Master env
ORKESTRA_AGENT_ADDR=0.0.0.0:4440
ORKESTRA_UI_ADDR=0.0.0.0:8080
ORKESTRA_PUBLIC_URL=https://orkestra.example.com   # startup default; overridable in the UI
ORKESTRA_AGENT_TLS_SANS=orkestra.example.com
```

Agent metrics need **no** inbound port on the agent hosts — they are federated through the
Master (see *Observability → Metrics*). The only agent-facing hole in the firewall is `4440`
on the Master.

### Co-locating the Master and an Agent on one host

The Master and an Agent can run on the same host without conflict:

- **No port clash:** the Master listens on `8080`/`4440`/`9090`; the Agent has no inbound
  listeners at all (it dials *outbound* to the Master), only a local metrics endpoint.
- **Docker socket:** only the Agent needs `/var/run/docker.sock`; the Master never touches Docker.
- **Loopback works out of the box:** a co-located agent enrolls with
  `--master https://localhost:4440` — loopback SANs are always present.
- **Caution:** a co-located agent controls the host's Docker daemon. If you assign it a stack that
  includes the Master's own containers, the agent can recreate or stop the Master underneath
  itself. Keep the Master's own stack off the co-located agent.

---

## Deployment: Agent

### install-agent.sh

`deploy/install-agent.sh` automates Agent installation on a new server:

```bash
#!/usr/bin/env bash
# Usage: ./install-agent.sh \
#   --master https://master.example.com:4440 \
#   --bootstrap-token <token> \
#   --name "web-server-01" \
#   [--version latest] \
#   [--data-dir /etc/orkestra/agent]

# Steps:
# 1. Detect OS/arch
# 2. Download the orkestra-agent archive from GitHub Releases
# 3. Verify the checksum
# 4. Install the binary and create directories + service user
# 5. Run: orkestra-agent enroll --master $MASTER --bootstrap-token $TOKEN --name $NAME
# 6. Write the env file and install the systemd unit
#    (prefers a non-root user when a 'docker' group exists)
# 7. Enable + start the service
```

### Agent Systemd Unit

`deploy/systemd/orkestra-agent.service`:

```ini
[Unit]
Description=orkestra Agent — Docker Host Controller
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=root                        # needs docker.sock access
ExecStart=/usr/bin/orkestra-agent serve
EnvironmentFile=-/etc/orkestra/agent/env
Restart=on-failure
RestartSec=10s

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/orkestra /etc/orkestra/agent /run/docker.sock /var/run/docker.sock
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictRealtime=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
```

`/etc/orkestra/agent/env`:
```
ORKESTRA_AGENT_DATA=/etc/orkestra/agent
ORKESTRA_AGENT_METRICS_ADDR=127.0.0.1:9091
ORKESTRA_LOG_LEVEL=info
# Only needed for container auto-enrolment (see below):
# ORKESTRA_MASTER_ADDR=https://master.example.com:4440
# ORKESTRA_BOOTSTRAP_TOKEN=<token>
# ORKESTRA_AGENT_NAME=web-01
```

The master address the agent dials is stored in `config.json` at enrollment time, so a normally
enrolled agent does not need `ORKESTRA_MASTER_ADDR` in its env file.

> **Note on `User=root`:** The Agent needs `/var/run/docker.sock` access. If the Docker socket
> is accessible to a `docker` group, `User=orkestra` with `SupplementaryGroups=docker` is
> preferred. The install script detects this and sets up accordingly.

### Agent in a container (TrueNAS SCALE & other Docker hosts)

The agent ships as a multi-arch image `ghcr.io/heckertobias/orkestra-agent`. In a container it
**auto-enrolls** on first boot when `ORKESTRA_MASTER_ADDR` + `ORKESTRA_BOOTSTRAP_TOKEN` are set
(the distroless image has no shell for an entrypoint script), then reuses the stored certificate
on restart. Requirements:

- Mount `/var/run/docker.sock` and run as a uid that can access it (root on TrueNAS).
- Persist `/var/lib/orkestra` (the image sets `ORKESTRA_AGENT_DATA` there) — it holds the cert.
- Enroll against the Master's agent port `:4440`.

For **TrueNAS SCALE** specifically — a ready-to-paste *Custom App* YAML and a guided *catalog
app* (with a labeled install form) live under [`deploy/truenas/`](../deploy/truenas/); see its
`README.md`.

---

## Release Pipeline

`goreleaser` configuration (`.goreleaser.yaml`) produces:

**Binaries:**
- `orkestra-master_linux_amd64`
- `orkestra-master_linux_arm64`
- `orkestra-agent_linux_amd64`
- `orkestra-agent_linux_arm64`

**Archives:** `.tar.gz` with the binary, systemd unit, and default env file.

**Packages:** `.deb` and `.rpm` for both binaries (amd64 + arm64), installing the systemd unit, a
default `/etc/orkestra/<tool>/env`, and a system user where needed.

**Docker images (multi-arch manifests, amd64 + arm64):**
- `ghcr.io/heckertobias/orkestra-master:{version}`
- `ghcr.io/heckertobias/orkestra-agent:{version}`

**CI (GitHub Actions, `.github/workflows/`):**

| Workflow | Jobs |
|---|---|
| `ci.yml` (push + PR) | `build-test` (codegen, `go vet`, `go test`, build) · `lint` (golangci-lint, buf lint) · `frontend` (npm build, eslint, vitest) · `integration` (tests against Postgres + Docker) |
| `codeql.yml` | CodeQL analysis |
| `release.yml` (tag `v*`) | goreleaser: binaries, archives, packages, images |
| `pages.yml` | Publishes the signed apt/rpm repository to GitHub Pages |

---

## Backup & Recovery

### What to Back Up

| Item | How | Frequency | Note |
|---|---|---|---|
| PostgreSQL database | `pg_dump` (see below) | Daily (at minimum) | — |
| KEK (master key file) | Copy `secrets/master_key` or `/etc/orkestra/master/master.key` | On creation; keep in a password manager / HSM | Must be stored **separately** from the DB backup |
| Reverse-proxy TLS certs | Whatever your proxy uses | On renewal | Not managed by orkestra |

### Recovery Procedure

1. Install Master binary (and a Postgres instance) on new host.
2. Restore the database from backup: `psql $ORKESTRA_DATABASE_URL < backup.sql`
3. Restore the KEK file from your separate backup to the same path
   (e.g. `/etc/orkestra/master/master.key`, `chmod 600`), and set `ORKESTRA_MASTER_KEY_FILE`
   accordingly.
4. Start Master — it picks up all servers, stacks, secrets from the restored DB.
5. Agents reconnect automatically (they have their certs; as long as the CA cert + DB are
   restored, mTLS still works).

### Postgres Backup (Live)

```bash
# Dump while Master is running (Postgres handles concurrent access natively)
pg_dump "$ORKESTRA_DATABASE_URL" > "backup/orkestra-$(date +%Y%m%d).sql"

# Or compressed:
pg_dump "$ORKESTRA_DATABASE_URL" -Fc -f "backup/orkestra-$(date +%Y%m%d).dump"
# Restore: pg_restore -d "$ORKESTRA_DATABASE_URL" backup/orkestra-20240101.dump
```
