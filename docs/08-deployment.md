# orkestra — Observability & Deployment

## Observability

### Logging

Both the Master and Agent use **`log/slog`** (Go stdlib structured logging).

- **Development:** human-readable text format (`slog.TextHandler`).
- **Production:** JSON format (`slog.JSONHandler`) — parseable by Loki, Fluentd, etc.

Log level is configurable via `--log-level` flag or `ORKESTRA_LOG_LEVEL` env var
(`debug | info | warn | error`; default: `info`).

Key log fields used consistently:
- `component`: `master.agentgw`, `master.reconciler`, `agent.reconcile`, `agent.dockerctl`, etc.
- `agent_id`: on all Agent-related log lines in the Master.
- `stack_id`: on reconcile-related lines.
- `request_id`: on gRPC stream message processing.

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
| `GET /readyz` | Readiness: 200 if DB is reachable + CA is loaded + gRPC endpoint is up |

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
      - "8080:8080"   # Web UI + API
    secrets:
      - orkestra_master_key
    # Uncomment to provide a TLS cert for the UI endpoint:
    # volumes:
    #   - ./tls:/tls:ro
    environment:
      # KEK is read from the secret mount — never set ORKESTRA_MASTER_KEY here
      ORKESTRA_MASTER_KEY_FILE: /run/secrets/orkestra_master_key
      ORKESTRA_DATABASE_URL: "postgres://orkestra:${POSTGRES_PASSWORD}@postgres:5432/orkestra?sslmode=disable"
      ORKESTRA_AGENT_ADDR: "0.0.0.0:4440"
      ORKESTRA_UI_ADDR: "0.0.0.0:8080"
      ORKESTRA_LOG_LEVEL: info
      # ORKESTRA_TLS_CERT: /tls/server.crt
      # ORKESTRA_TLS_KEY:  /tls/server.key

volumes:
  postgres-data:
```

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
Description=orkestra Master
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=orkestra
Group=orkestra
ExecStart=/usr/local/bin/orkestra-master
EnvironmentFile=/etc/orkestra/master/env
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/orkestra

[Install]
WantedBy=multi-user.target
```

`/etc/orkestra/master/env` (DB credentials only — no KEK here):
```
ORKESTRA_DATABASE_URL=postgres://orkestra:<password>@localhost:5432/orkestra?sslmode=disable
ORKESTRA_AGENT_ADDR=0.0.0.0:4440
ORKESTRA_UI_ADDR=0.0.0.0:8080
ORKESTRA_MASTER_KEY_FILE=/etc/orkestra/master/master.key
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
  and forwards to `:8080`. Standard HTTPS.
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
- **Caution:** a co-located agent controls the host's Docker daemon. If you assign it a stack
  that includes the Master's own containers, the agent could recreate/stop the Master. Keep the
  Master's stack off the co-located agent until fleet-managed self-updates land (see
  `docs/09-updates.md`).

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
#   [--version latest]

# Steps:
# 1. Detect OS/arch
# 2. Download orkestra-agent binary from GitHub Releases
# 3. Verify checksum
# 4. Place at /usr/local/bin/orkestra-agent
# 5. Run: orkestra-agent enroll --master $MASTER --bootstrap-token $TOKEN --name $NAME
# 6. Install systemd service
# 7. Enable + start service
```

### Agent Systemd Unit

`deploy/systemd/orkestra-agent.service`:

```ini
[Unit]
Description=orkestra Agent
After=docker.service
Wants=docker.service

[Service]
Type=simple
User=root                        # needs docker.sock access
ExecStart=/usr/local/bin/orkestra-agent serve
EnvironmentFile=/etc/orkestra/agent/env
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

`/etc/orkestra/agent/env`:
```
ORKESTRA_MASTER_ADDR=https://master.example.com:4440
ORKESTRA_AGENT_DATA=/etc/orkestra/agent
ORKESTRA_LOG_LEVEL=info
```

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

**Archives:** `.tar.gz` with binary + systemd units + install script.

**Docker images:**
- `ghcr.io/heckertobias/orkestra-master:{version}`
- Multi-arch manifest (amd64 + arm64)

**CI (GitHub Actions, `.github/workflows/`):**

Pipeline jobs: `build` + `test` + `lint` (push + PR) → `release` (tag `v*` only).

---

## Backup & Recovery

### What to Back Up

| Item | How | Frequency | Note |
|---|---|---|---|
| PostgreSQL database | `pg_dump` (see below) | Daily (at minimum) | — |
| KEK (master key file) | Copy `secrets/master_key` or `/etc/orkestra/master/master.key` | On creation; keep in password manager / HSM | Must be stored **separately** from the DB backup |
| TLS certs (if self-managed) | Copy `/etc/orkestra/master/tls/` | On renewal | — |

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
