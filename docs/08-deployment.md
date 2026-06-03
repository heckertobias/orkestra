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

`/metrics` endpoint on `:9090` (Master) and `:9091` (Agent, configurable).

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
      - "8443:8443"   # Agent gRPC (mTLS)
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
      ORKESTRA_AGENT_ADDR: "0.0.0.0:8443"
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
ORKESTRA_AGENT_ADDR=0.0.0.0:8443
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

## Deployment: Agent

### install-agent.sh

`deploy/install-agent.sh` automates Agent installation on a new server:

```bash
#!/usr/bin/env bash
# Usage: ./install-agent.sh \
#   --master https://master.example.com:8443 \
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
ORKESTRA_MASTER_ADDR=https://master.example.com:8443
ORKESTRA_AGENT_DATA=/etc/orkestra/agent
ORKESTRA_LOG_LEVEL=info
```

> **Note on `User=root`:** The Agent needs `/var/run/docker.sock` access. If the Docker socket
> is accessible to a `docker` group, `User=orkestra` with `SupplementaryGroups=docker` is
> preferred. The install script detects this and sets up accordingly.

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
