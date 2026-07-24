#!/usr/bin/env bash
# Dev/test launcher: starts Postgres (Docker), builds the dev binary,
# starts the Master and the Vite dev server.
# All background processes AND Docker containers are removed when this script exits
# (Ctrl+C or process death) — each run starts with a fresh database.
# Copy .env.example → .env to override defaults.
#
# Optional services (off by default):
#   --keycloak   Start a pre-configured Keycloak IdP for OIDC testing
#   --mailpit    Start Mailpit SMTP catcher for email testing
#   --all        Start all optional services
#   --agent      Start one orkestra agent (isolated Docker-in-Docker daemon)
#   --agents N   Start N agents, each with its own isolated DinD daemon
#
# These can also be enabled via env vars: ORKESTRA_DEV_KEYCLOAK=1, ORKESTRA_DEV_MAILPIT=1,
# ORKESTRA_DEV_AGENTS=N
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

# ── Parse flags ───────────────────────────────────────────────────────────────

START_KEYCLOAK="${ORKESTRA_DEV_KEYCLOAK:-0}"
START_MAILPIT="${ORKESTRA_DEV_MAILPIT:-0}"
AGENT_COUNT="${ORKESTRA_DEV_AGENTS:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keycloak) START_KEYCLOAK=1 ;;
    --mailpit|--smtp) START_MAILPIT=1 ;;
    --all) START_KEYCLOAK=1; START_MAILPIT=1 ;;
    --agent) AGENT_COUNT=1 ;;
    --agents) AGENT_COUNT="${2:-}"; shift ;;
    --agents=*) AGENT_COUNT="${1#*=}" ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
  shift
done

if ! [[ "$AGENT_COUNT" =~ ^[0-9]+$ ]]; then
  echo "✗ Invalid agent count: '$AGENT_COUNT' (use --agent or --agents N)"
  exit 1
fi

# ── Load .env if present ──────────────────────────────────────────────────────

if [[ -f .env ]]; then
  echo "→ Loading .env"
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

# ── Config (all overridable via .env / environment) ───────────────────────────

DB_CONTAINER="${ORKESTRA_DEV_DB_CONTAINER:-orkestra-dev-pg}"
DB_DSN="${ORKESTRA_DATABASE_URL:-postgres://orkestra:orkestra@localhost:5432/orkestra?sslmode=disable}"
MASTER_KEY="${ORKESTRA_MASTER_KEY:-0000000000000000000000000000000000000000000000000000000000000001}"
UI_ADDR="${ORKESTRA_UI_ADDR:-0.0.0.0:8080}"
AGENT_ADDR="${ORKESTRA_AGENT_ADDR:-0.0.0.0:4440}"
METRICS_ADDR="${ORKESTRA_METRICS_ADDR:-0.0.0.0:9090}"
VITE_PORT="${ORKESTRA_VITE_PORT:-5173}"

# Optional-service config
KEYCLOAK_CONTAINER="${ORKESTRA_DEV_KEYCLOAK_CONTAINER:-orkestra-dev-keycloak}"
KEYCLOAK_PORT="${ORKESTRA_DEV_KEYCLOAK_PORT:-8180}"
KEYCLOAK_IMAGE="${ORKESTRA_DEV_KEYCLOAK_IMAGE:-quay.io/keycloak/keycloak:26.0}"
KEYCLOAK_ADMIN="${ORKESTRA_DEV_KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_ADMIN_PASSWORD="${ORKESTRA_DEV_KEYCLOAK_ADMIN_PASSWORD:-admin}"

MAILPIT_CONTAINER="${ORKESTRA_DEV_MAILPIT_CONTAINER:-orkestra-dev-mailpit}"
MAILPIT_SMTP_PORT="${ORKESTRA_DEV_MAILPIT_SMTP_PORT:-2525}"
MAILPIT_UI_PORT="${ORKESTRA_DEV_MAILPIT_UI_PORT:-8025}"
MAILPIT_IMAGE="${ORKESTRA_DEV_MAILPIT_IMAGE:-axllent/mailpit:latest}"

# Dev admin — created automatically on every first run via the setup token
DEV_ADMIN_EMAIL="${ORKESTRA_DEV_ADMIN_EMAIL:-admin@orkestra.local}"
DEV_ADMIN_PASSWORD="${ORKESTRA_DEV_ADMIN_PASSWORD:-orkestra-dev}"
COOKIE_JAR="/tmp/orkestra-dev-cookies.txt"

MASTER_LOG="/tmp/orkestra-master.log"
VITE_LOG="/tmp/orkestra-vite.log"

# Extract just the port numbers for the port-conflict check
UI_PORT="${UI_ADDR##*:}"
AGENT_PORT="${AGENT_ADDR##*:}"
METRICS_PORT="${METRICS_ADDR##*:}"

# Agent (Docker-in-Docker) config — each agent gets its own isolated dockerd so test stacks
# never touch the host's Docker. Agents run as host processes (from make build-dev) and reach
# their daemon over DOCKER_HOST and the Master over the local mTLS port.
AGENT_MASTER_ADDR="${ORKESTRA_DEV_AGENT_MASTER:-https://localhost:${AGENT_PORT}}"
DIND_IMAGE="${ORKESTRA_DEV_DIND_IMAGE:-docker:dind}"
DIND_BASE_PORT="${ORKESTRA_DEV_DIND_BASE_PORT:-23751}"
AGENT_METRICS_BASE_PORT="${ORKESTRA_DEV_AGENT_METRICS_BASE_PORT:-9091}"
AGENT_CONTAINER_PREFIX="orkestra-dev-agent"
AGENT_DATA_PREFIX="/tmp/orkestra-dev-agent"
AGENT_PIDS=()

MASTER_PID=""
VITE_PID=""

# ── Cleanup ───────────────────────────────────────────────────────────────────

cleanup() {
  echo ""
  echo "→ Shutting down..."
  [[ -n "$MASTER_PID" ]] && kill "$MASTER_PID" 2>/dev/null || true
  [[ -n "$VITE_PID"   ]] && kill "$VITE_PID"   2>/dev/null || true
  if [[ ${#AGENT_PIDS[@]} -gt 0 ]]; then
    for pid in "${AGENT_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  fi
  wait 2>/dev/null || true
  echo "→ Removing dev containers..."
  docker rm -f "$DB_CONTAINER" 2>/dev/null || true
  if [[ "$START_MAILPIT"  == "1" ]]; then docker rm -f "$MAILPIT_CONTAINER"  2>/dev/null || true; fi
  if [[ "$START_KEYCLOAK" == "1" ]]; then docker rm -f "$KEYCLOAK_CONTAINER" 2>/dev/null || true; fi
  if [[ "$AGENT_COUNT" -gt 0 ]]; then
    for ((i=1; i<=AGENT_COUNT; i++)); do
      docker rm -f "${AGENT_CONTAINER_PREFIX}-${i}-dind" 2>/dev/null || true
      rm -rf "${AGENT_DATA_PREFIX}-${i}" 2>/dev/null || true
    done
  fi
  rm -f "$COOKIE_JAR"
  echo "→ Done. (All dev containers removed — next start gets a fresh DB.)"
}
trap cleanup EXIT INT TERM

# ── Port check ────────────────────────────────────────────────────────────────

PORTS_TO_CHECK=("$UI_PORT" "$AGENT_PORT" "$METRICS_PORT" "$VITE_PORT")
[[ "$START_KEYCLOAK" == "1" ]] && PORTS_TO_CHECK+=("$KEYCLOAK_PORT")
[[ "$START_MAILPIT"  == "1" ]] && PORTS_TO_CHECK+=("$MAILPIT_SMTP_PORT" "$MAILPIT_UI_PORT")
if [[ "$AGENT_COUNT" -gt 0 ]]; then
  for ((i=1; i<=AGENT_COUNT; i++)); do
    PORTS_TO_CHECK+=("$((DIND_BASE_PORT + i - 1))" "$((AGENT_METRICS_BASE_PORT + i - 1))")
  done
fi

BLOCKED=""
for port in "${PORTS_TO_CHECK[@]}"; do
  pid="$(lsof -ti tcp:"$port" 2>/dev/null || true)"
  if [[ -n "$pid" ]]; then
    cmd="$(ps -p "$pid" -o comm= 2>/dev/null || echo '?')"
    BLOCKED="$BLOCKED\n  Port $port → pid $pid ($cmd)   kill: kill $pid"
  fi
done

if [[ -n "$BLOCKED" ]]; then
  echo "✗ The following ports are already in use:"
  printf "$BLOCKED\n"
  echo ""
  echo "  Stop the conflicting processes and try again."
  echo "  Or override ports in .env (see .env.example)."
  exit 1
fi

# ── Postgres ──────────────────────────────────────────────────────────────────

if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${DB_CONTAINER}$"; then
  echo "→ Postgres already running ($DB_CONTAINER)"
else
  if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^${DB_CONTAINER}$"; then
    echo "→ Starting existing postgres container..."
    docker start "$DB_CONTAINER" > /dev/null
  else
    echo "→ Creating postgres container..."
    docker run -d \
      --name "$DB_CONTAINER" \
      -e POSTGRES_DB=orkestra \
      -e POSTGRES_USER=orkestra \
      -e POSTGRES_PASSWORD=orkestra \
      -p 5432:5432 \
      postgres:16-alpine > /dev/null
  fi
  echo -n "→ Waiting for postgres to accept connections..."
  until docker exec "$DB_CONTAINER" pg_isready -U orkestra -d orkestra -q 2>/dev/null; do
    echo -n "."
    sleep 1
  done
  echo " ready."
fi

# ── Mailpit ───────────────────────────────────────────────────────────────────

if [[ "$START_MAILPIT" == "1" ]]; then
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${MAILPIT_CONTAINER}$"; then
    echo "→ Mailpit already running ($MAILPIT_CONTAINER)"
  else
    if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^${MAILPIT_CONTAINER}$"; then
      echo "→ Starting existing Mailpit container..."
      docker start "$MAILPIT_CONTAINER" > /dev/null
    else
      echo "→ Creating Mailpit container..."
      docker run -d \
        --name "$MAILPIT_CONTAINER" \
        -p "${MAILPIT_SMTP_PORT}:1025" \
        -p "${MAILPIT_UI_PORT}:8025" \
        "$MAILPIT_IMAGE" > /dev/null
    fi
    echo -n "→ Waiting for Mailpit..."
    for _ in $(seq 1 20); do
      curl -sf "http://localhost:${MAILPIT_UI_PORT}/api/v1/info" > /dev/null 2>&1 && break
      echo -n "."
      sleep 1
    done
    echo " ready."
  fi
fi

# ── Keycloak ──────────────────────────────────────────────────────────────────

if [[ "$START_KEYCLOAK" == "1" ]]; then
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${KEYCLOAK_CONTAINER}$"; then
    echo "→ Keycloak already running ($KEYCLOAK_CONTAINER)"
  else
    if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -q "^${KEYCLOAK_CONTAINER}$"; then
      echo "→ Starting existing Keycloak container..."
      docker start "$KEYCLOAK_CONTAINER" > /dev/null
    else
      echo "→ Creating Keycloak container (first boot takes ~30 s)..."
      docker run -d \
        --name "$KEYCLOAK_CONTAINER" \
        -p "${KEYCLOAK_PORT}:8080" \
        -e KC_BOOTSTRAP_ADMIN_USERNAME="$KEYCLOAK_ADMIN" \
        -e KC_BOOTSTRAP_ADMIN_PASSWORD="$KEYCLOAK_ADMIN_PASSWORD" \
        -v "${REPO_ROOT}/dev/keycloak:/opt/keycloak/data/import:ro" \
        "$KEYCLOAK_IMAGE" \
        start-dev --import-realm > /dev/null
    fi
    echo -n "→ Waiting for Keycloak realm to be ready (this may take ~40 s on first boot)..."
    for _ in $(seq 1 60); do
      if curl -sf "http://localhost:${KEYCLOAK_PORT}/realms/orkestra/.well-known/openid-configuration" > /dev/null 2>&1; then
        break
      fi
      echo -n "."
      sleep 2
    done
    if ! curl -sf "http://localhost:${KEYCLOAK_PORT}/realms/orkestra/.well-known/openid-configuration" > /dev/null 2>&1; then
      echo ""
      echo "✗ Keycloak did not become ready in time. Check: docker logs $KEYCLOAK_CONTAINER"
      exit 1
    fi
    echo " ready."
  fi
fi

# ── Build ─────────────────────────────────────────────────────────────────────

echo "→ Building (dev)..."
make build-dev

# ── Master ────────────────────────────────────────────────────────────────────

export ORKESTRA_DATABASE_URL="$DB_DSN"
export ORKESTRA_MASTER_KEY="$MASTER_KEY"
export ORKESTRA_UI_ADDR="$UI_ADDR"
export ORKESTRA_AGENT_ADDR="$AGENT_ADDR"
export ORKESTRA_METRICS_ADDR="$METRICS_ADDR"
# Dev serves the UI over plain HTTP (http://localhost:8080), so cookies must not be
# Secure-only or the browser would drop them. Secure cookies default to on in production.
export ORKESTRA_SECURE_COOKIES="${ORKESTRA_SECURE_COOKIES:-false}"

echo "→ Starting Master (logs → $MASTER_LOG)"
./bin/orkestra-master --log-level debug > "$MASTER_LOG" 2>&1 &
MASTER_PID=$!

echo -n "→ Waiting for Master to be ready..."
for _ in $(seq 1 30); do
  if ! kill -0 "$MASTER_PID" 2>/dev/null; then
    echo ""
    echo "✗ Master crashed. Last lines:"
    tail -20 "$MASTER_LOG"
    exit 1
  fi
  curl -sf "http://localhost:${UI_PORT}/healthz" > /dev/null 2>&1 && break
  echo -n "."
  sleep 1
done
echo " ready."

# ── Auto-configure dev instance ───────────────────────────────────────────────
# Uses the first-run setup token from the master log to create the dev admin,
# then applies Mailpit / Keycloak settings via the admin API.

auto_configure() {
  # Extract the setup token — slog format: "... url=http://…/login?setup=TOKEN"
  local setup_token
  setup_token="$(grep -o 'setup=[^ "]*' "$MASTER_LOG" 2>/dev/null | head -1 | sed 's/setup=//' || true)"

  if [[ -z "$setup_token" ]]; then
    echo "  ⚠ No setup token found in master log — skipping auto-config"
    return 0
  fi

  # Create the dev admin account
  echo "  → Creating dev admin (${DEV_ADMIN_EMAIL})..."
  if ! curl -sf -X POST \
      -H "Content-Type: application/json" \
      -d "{\"token\":\"${setup_token}\",\"username\":\"${DEV_ADMIN_EMAIL}\",\"password\":\"${DEV_ADMIN_PASSWORD}\",\"displayName\":\"Dev Admin\"}" \
      "http://localhost:${UI_PORT}/api/setup" > /dev/null; then
    echo "  ⚠ /api/setup failed — skipping auto-config"
    return 0
  fi
  echo "  ✓ Dev admin created"

  # Log in and capture the session cookie
  echo "  → Logging in..."
  rm -f "$COOKIE_JAR"
  if ! curl -sf -c "$COOKIE_JAR" \
      -H "Content-Type: application/json" \
      -H "Connect-Protocol-Version: 1" \
      -d "{\"username\":\"${DEV_ADMIN_EMAIL}\",\"password\":\"${DEV_ADMIN_PASSWORD}\"}" \
      "http://localhost:${UI_PORT}/orkestra.v1.AuthService/Login" > /dev/null; then
    echo "  ⚠ Login failed — skipping service config"
    return 0
  fi
  echo "  ✓ Logged in"

  # Set the deployment-wide public URL (used for OIDC redirect, email, and setup links).
  if curl -sf -b "$COOKIE_JAR" \
      -H "Content-Type: application/json" \
      -H "Connect-Protocol-Version: 1" \
      -d "{\"public_url\":\"http://localhost:${UI_PORT}\"}" \
      "http://localhost:${UI_PORT}/orkestra.v1.AuthService/UpdateServerConfig" > /dev/null; then
    echo "  ✓ Public URL set"
  else
    echo "  ⚠ UpdateServerConfig failed — set manually in Settings → General"
  fi

  # Configure SMTP via Mailpit
  if [[ "$START_MAILPIT" == "1" ]]; then
    echo "  → Configuring SMTP (Mailpit on port ${MAILPIT_SMTP_PORT})..."
    if curl -sf -b "$COOKIE_JAR" \
        -H "Content-Type: application/json" \
        -H "Connect-Protocol-Version: 1" \
        -d "{\"enabled\":true,\"host\":\"localhost\",\"port\":${MAILPIT_SMTP_PORT},\"username\":\"\",\"password\":\"\",\"fromAddress\":\"orkestra@dev.local\",\"starttls\":false}" \
        "http://localhost:${UI_PORT}/orkestra.v1.AuthService/UpdateSMTPConfig" > /dev/null; then
      echo "  ✓ SMTP configured"
    else
      echo "  ⚠ UpdateSMTPConfig failed — configure manually in Settings → Email"
    fi
  fi

  # Configure OIDC via Keycloak
  if [[ "$START_KEYCLOAK" == "1" ]]; then
    echo "  → Configuring OIDC (Keycloak at http://localhost:${KEYCLOAK_PORT}/realms/orkestra)..."
    if curl -sf -b "$COOKIE_JAR" \
        -H "Content-Type: application/json" \
        -H "Connect-Protocol-Version: 1" \
        -d "{\"enabled\":true,\"issuerUrl\":\"http://localhost:${KEYCLOAK_PORT}/realms/orkestra\",\"clientId\":\"orkestra\",\"clientSecret\":\"orkestra-dev-secret\",\"groupsClaim\":\"groups\"}" \
        "http://localhost:${UI_PORT}/orkestra.v1.AuthService/UpdateOIDCConfig" > /dev/null; then
      echo "  ✓ OIDC configured"
    else
      echo "  ⚠ UpdateOIDCConfig failed — configure manually in Settings → OIDC"
    fi
  fi

  echo "  ✓ Auto-configuration complete"
}

echo "→ Auto-configuring dev instance..."
auto_configure

# ── Agents (Docker-in-Docker) ─────────────────────────────────────────────────
# Each agent runs against its own isolated dockerd (a privileged docker:dind container),
# enrolls with a freshly minted bootstrap token, and connects to the Master over mTLS.

start_agents() {
  echo "→ Starting ${AGENT_COUNT} agent(s) with isolated Docker-in-Docker daemons..."

  # Mint a multi-use enrollment token via the admin API (reuses the login cookie).
  local token_resp raw_token
  token_resp="$(curl -sf -b "$COOKIE_JAR" \
      -H "Content-Type: application/json" \
      -H "Connect-Protocol-Version: 1" \
      -d "{\"description\":\"dev harness\",\"ttlSeconds\":3600,\"maxUses\":$((AGENT_COUNT + 5))}" \
      "http://localhost:${UI_PORT}/orkestra.v1.AuthService/CreateEnrollmentToken" || true)"
  raw_token="$(printf '%s' "$token_resp" | grep -o '"rawToken":"[^"]*"' | head -1 | sed 's/.*"rawToken":"//;s/"$//' || true)"
  if [[ -z "$raw_token" ]]; then
    echo "  ⚠ Could not mint enrollment token — skipping agents."
    echo "    Response: $token_resp"
    return 0
  fi
  echo "  ✓ Enrollment token minted"

  for ((i=1; i<=AGENT_COUNT; i++)); do
    local cname="${AGENT_CONTAINER_PREFIX}-${i}-dind"
    local ddir="${AGENT_DATA_PREFIX}-${i}"
    local dport="$((DIND_BASE_PORT + i - 1))"
    local mport="$((AGENT_METRICS_BASE_PORT + i - 1))"
    local alog="/tmp/orkestra-agent-${i}.log"

    echo "  → [agent ${i}] starting DinD daemon (${cname}, docker :${dport})..."
    docker rm -f "$cname" >/dev/null 2>&1 || true
    if ! docker run -d --privileged --name "$cname" \
        -e DOCKER_TLS_CERTDIR="" \
        -p "${dport}:2375" \
        "$DIND_IMAGE" --host=tcp://0.0.0.0:2375 >/dev/null; then
      echo "  ⚠ could not start DinD for agent ${i} — skipping"
      continue
    fi

    echo -n "  → [agent ${i}] waiting for DinD daemon..."
    local ready=0
    for _ in $(seq 1 30); do
      if docker -H "tcp://localhost:${dport}" info >/dev/null 2>&1; then ready=1; break; fi
      echo -n "."
      sleep 1
    done
    if [[ "$ready" != "1" ]]; then
      echo " ✗ not ready — skipping agent ${i} (check: docker logs ${cname})"
      docker rm -f "$cname" >/dev/null 2>&1 || true
      continue
    fi
    echo " ready."

    # Fresh data dir, then enroll (writes agent.crt/agent.key/ca.crt/config.json).
    rm -rf "$ddir"; mkdir -p "$ddir"
    echo "  → [agent ${i}] enrolling as dev-agent-${i}..."
    if ! ./bin/orkestra-agent enroll \
        --master "$AGENT_MASTER_ADDR" \
        --bootstrap-token "$raw_token" \
        --name "dev-agent-${i}" \
        --data-dir "$ddir" \
        --log-level debug >> "$alog" 2>&1; then
      echo "  ⚠ enrollment failed for agent ${i} — see ${alog}"
      docker rm -f "$cname" >/dev/null 2>&1 || true
      continue
    fi

    # Serve, pointing the agent at its own isolated DinD daemon.
    echo "  → [agent ${i}] starting (logs → ${alog})"
    DOCKER_HOST="tcp://localhost:${dport}" \
    ORKESTRA_AGENT_METRICS_ADDR="0.0.0.0:${mport}" \
      ./bin/orkestra-agent serve --data-dir "$ddir" --log-level debug >> "$alog" 2>&1 &
    AGENT_PIDS+=("$!")
    echo "  ✓ [agent ${i}] running (pid $!, docker via tcp://localhost:${dport})"
  done
}

if [[ "$AGENT_COUNT" -gt 0 ]]; then
  start_agents
fi

# ── Vite ─────────────────────────────────────────────────────────────────────

echo "→ Starting Vite dev server (logs → $VITE_LOG)"
(cd web && ORKESTRA_UI_PORT="$UI_PORT" VITE_PORT="$VITE_PORT" npm run dev -- --port "$VITE_PORT") > "$VITE_LOG" 2>&1 &
VITE_PID=$!

sleep 2
if ! kill -0 "$VITE_PID" 2>/dev/null; then
  echo "✗ Vite failed to start. Last lines:"
  tail -20 "$VITE_LOG"
  exit 1
fi

# ── Ready banner ──────────────────────────────────────────────────────────────

echo ""
echo "┌──────────────────────────────────────────────────┐"
echo "│  orkestra dev instance running                   │"
echo "│                                                  │"
printf "│  UI:      http://localhost:%-22s│\n" "${UI_PORT}      "
printf "│  Vite:    http://localhost:%-22s│\n" "${VITE_PORT}      "
printf "│  Metrics: http://localhost:%-22s│\n" "${METRICS_PORT}/metrics  "

if [[ "$START_MAILPIT" == "1" ]]; then
  echo "│                                                  │"
  printf "│  Mailpit: http://localhost:%-22s│\n" "${MAILPIT_UI_PORT}          "
fi

if [[ "$START_KEYCLOAK" == "1" ]]; then
  printf "│  Keycloak: http://localhost:%-21s│\n" "${KEYCLOAK_PORT}         "
fi

if [[ ${#AGENT_PIDS[@]} -gt 0 ]]; then
  printf "│  Agents:   %-38s│\n" "${#AGENT_PIDS[@]} running (isolated DinD)"
fi

echo "│                                                  │"
echo "│  Ctrl+C to stop everything                      │"
echo "└──────────────────────────────────────────────────┘"

echo ""
echo "┌─ Dev admin ──────────────────────────────────────┐"
echo "│                                                  │"
printf "│  Email:    %-38s│\n" "$DEV_ADMIN_EMAIL"
printf "│  Password: %-38s│\n" "$DEV_ADMIN_PASSWORD"
echo "│                                                  │"
echo "└──────────────────────────────────────────────────┘"

if [[ ${#AGENT_PIDS[@]} -gt 0 ]]; then
  echo ""
  echo "┌─ Agents (isolated Docker-in-Docker) ─────────────┐"
  echo "│                                                  │"
  for ((i=1; i<=AGENT_COUNT; i++)); do
    dport="$((DIND_BASE_PORT + i - 1))"
    printf "│  dev-agent-%-2s log: /tmp/orkestra-agent-%-2s.log   │\n" "${i}" "${i}"
    printf "│    inspect: docker -H tcp://localhost:%-11s│\n" "${dport} ps"
  done
  echo "│                                                  │"
  echo "│  Agents appear on the Servers page once online.  │"
  echo "│  Their stacks run ONLY in their own DinD daemon. │"
  echo "└──────────────────────────────────────────────────┘"
fi

if [[ "$START_MAILPIT" == "1" ]]; then
  echo ""
  echo "┌─ Mailpit (SMTP auto-configured) ─────────────────┐"
  echo "│                                                  │"
  printf "│  Inbox: http://localhost:%-24s│\n" "${MAILPIT_UI_PORT}"
  echo "│                                                  │"
  echo "│  All outgoing emails are caught here.            │"
  echo "└──────────────────────────────────────────────────┘"
fi

if [[ "$START_KEYCLOAK" == "1" ]]; then
  echo ""
  echo "┌─ Keycloak (OIDC auto-configured) ────────────────┐"
  echo "│                                                  │"
  echo "│  SSO login enabled via Keycloak.                 │"
  echo "│                                                  │"
  echo "│  Test user: testuser@example.com / test          │"
  echo "│  !! Pre-create this user in orkestra first !!    │"
  echo "│                                                  │"
  printf "│  Keycloak admin: http://localhost:%-15s│\n" "${KEYCLOAK_PORT}"
  printf "│    login: %-39s│\n" "${KEYCLOAK_ADMIN} / ${KEYCLOAK_ADMIN_PASSWORD}"
  echo "└──────────────────────────────────────────────────┘"
fi

echo ""

# ── Keep alive until a process dies or user hits Ctrl+C ───────────────────────

while kill -0 "$MASTER_PID" 2>/dev/null && kill -0 "$VITE_PID" 2>/dev/null; do
  sleep 2
done

if ! kill -0 "$MASTER_PID" 2>/dev/null; then
  echo "✗ Master exited unexpectedly. Last lines:"
  tail -20 "$MASTER_LOG"
fi
if ! kill -0 "$VITE_PID" 2>/dev/null; then
  echo "✗ Vite exited unexpectedly. Last lines:"
  tail -20 "$VITE_LOG"
fi
exit 1
