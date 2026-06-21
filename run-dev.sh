#!/usr/bin/env bash
# Dev/test launcher: starts Postgres (Docker), builds the dev binary,
# starts the Master and the Vite dev server.
# All background processes are stopped when this script exits (Ctrl+C or process death).
# Copy .env.example → .env to override defaults.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

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
AGENT_ADDR="${ORKESTRA_AGENT_ADDR:-0.0.0.0:8443}"
METRICS_ADDR="${ORKESTRA_METRICS_ADDR:-0.0.0.0:9090}"
VITE_PORT="${ORKESTRA_VITE_PORT:-5173}"

MASTER_LOG="/tmp/orkestra-master.log"
VITE_LOG="/tmp/orkestra-vite.log"

# Extract just the port numbers for the port-conflict check
UI_PORT="${UI_ADDR##*:}"
AGENT_PORT="${AGENT_ADDR##*:}"
METRICS_PORT="${METRICS_ADDR##*:}"

MASTER_PID=""
VITE_PID=""

# ── Cleanup ───────────────────────────────────────────────────────────────────

cleanup() {
  echo ""
  echo "→ Shutting down..."
  [[ -n "$MASTER_PID" ]] && kill "$MASTER_PID" 2>/dev/null || true
  [[ -n "$VITE_PID"   ]] && kill "$VITE_PID"   2>/dev/null || true
  wait 2>/dev/null || true
  echo "→ Done."
}
trap cleanup EXIT INT TERM

# ── Port check ────────────────────────────────────────────────────────────────

BLOCKED=""
for port in "$UI_PORT" "$AGENT_PORT" "$METRICS_PORT" "$VITE_PORT"; do
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

# ── Build ─────────────────────────────────────────────────────────────────────

echo "→ Building (dev)..."
make build-dev

# ── Master ────────────────────────────────────────────────────────────────────

export ORKESTRA_DATABASE_URL="$DB_DSN"
export ORKESTRA_MASTER_KEY="$MASTER_KEY"
export ORKESTRA_UI_ADDR="$UI_ADDR"
export ORKESTRA_AGENT_ADDR="$AGENT_ADDR"
export ORKESTRA_METRICS_ADDR="$METRICS_ADDR"

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

SETUP_URL="$(grep -o 'url=http[^ ]*' "$MASTER_LOG" 2>/dev/null | head -1 | sed 's/url=//' | sed "s|0\.0\.0\.0|localhost|" || true)"

echo ""
echo "┌──────────────────────────────────────────────────┐"
echo "│  orkestra dev instance running                   │"
echo "│                                                  │"
printf "│  UI:      http://localhost:%-22s│\n" "${UI_PORT}      "
printf "│  Vite:    http://localhost:%-22s│\n" "${VITE_PORT}      "
printf "│  Metrics: http://localhost:%-22s│\n" "${METRICS_PORT}/metrics  "
echo "│                                                  │"
echo "│  Ctrl+C to stop everything                      │"
echo "└──────────────────────────────────────────────────┘"

if [[ -n "$SETUP_URL" ]]; then
  echo ""
  echo "  ★ First run — create your admin account:"
  echo "  $SETUP_URL"
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
