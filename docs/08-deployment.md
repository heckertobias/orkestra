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
services:
  master:
    image: ghcr.io/heckertobias/orkestra-master:latest
    restart: unless-stopped
    ports:
      - "8443:8443"   # Agent gRPC (mTLS)
      - "8080:8080"   # Web UI + API
    volumes:
      - orkestra-data:/data
      - ./tls:/tls:ro            # TLS cert for :8080 (optional)
    environment:
      ORKESTRA_MASTER_KEY: "${ORKESTRA_MASTER_KEY}"
      ORKESTRA_DB_PATH: /data/orkestra.db
      ORKESTRA_AGENT_ADDR: "0.0.0.0:8443"
      ORKESTRA_UI_ADDR: "0.0.0.0:8080"
      ORKESTRA_TLS_CERT: /tls/server.crt   # optional
      ORKESTRA_TLS_KEY: /tls/server.key    # optional
      ORKESTRA_LOG_LEVEL: info
volumes:
  orkestra-data:
```

First run:
```bash
export ORKESTRA_MASTER_KEY=$(openssl rand -hex 32)
echo "ORKESTRA_MASTER_KEY=$ORKESTRA_MASTER_KEY" >> .env
# Save this key somewhere safe!
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

`/etc/orkestra/master/env`:
```
ORKESTRA_MASTER_KEY=<hex-key>
ORKESTRA_DB_PATH=/var/lib/orkestra/orkestra.db
ORKESTRA_AGENT_ADDR=0.0.0.0:8443
ORKESTRA_UI_ADDR=0.0.0.0:8080
```

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

| Item | Location | Frequency |
|---|---|---|
| SQLite database | `ORKESTRA_DB_PATH` | Daily (at minimum) |
| Master key | `ORKESTRA_MASTER_KEY` | On creation, stored in password manager / HSM |
| TLS certs (if self-managed) | `/etc/orkestra/master/tls/` | On renewal |

### Recovery Procedure

1. Install Master binary on new host.
2. Restore `orkestra.db` from backup.
3. Set `ORKESTRA_MASTER_KEY` to the same value as before.
4. Start Master — it picks up all servers, stacks, secrets from the restored DB.
5. Agents reconnect automatically (they have their certs; as long as the CA cert + DB are
   restored, mTLS still works).

### SQLite Backup (Live)

```bash
# Online backup (safe while Master is running — SQLite WAL mode)
sqlite3 /var/lib/orkestra/orkestra.db ".backup '/backup/orkestra-$(date +%Y%m%d).db'"
```

The Master sets `PRAGMA journal_mode=WAL` on startup for concurrent read safety.
