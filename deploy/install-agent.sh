#!/usr/bin/env bash
# orkestra Agent installer
# Usage: ./install-agent.sh \
#   --master https://master.example.com:4440 \
#   --bootstrap-token <token> \
#   --name "web-server-01" \
#   [--version latest] \
#   [--data-dir /etc/orkestra/agent]
#
# Requires: curl, systemd, root or sudo

set -euo pipefail

MASTER=""
TOKEN=""
NAME=""
VERSION="latest"
DATA_DIR="/etc/orkestra/agent"
BINARY_PATH="/usr/bin/orkestra-agent"
GITHUB_REPO="heckertobias/orkestra"

# ─── Parse args ───────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --master)          MASTER="$2";   shift 2 ;;
    --bootstrap-token) TOKEN="$2";    shift 2 ;;
    --name)            NAME="$2";     shift 2 ;;
    --version)         VERSION="$2";  shift 2 ;;
    --data-dir)        DATA_DIR="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

[[ -z "$MASTER" ]] && { echo "Error: --master is required"; exit 1; }
[[ -z "$TOKEN"  ]] && { echo "Error: --bootstrap-token is required"; exit 1; }
[[ -z "$NAME"   ]] && NAME=$(hostname -f)

# ─── Detect OS / Arch ─────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Installing orkestra-agent ${VERSION} for ${OS}/${ARCH}"

# ─── Download binary ──────────────────────────────────────────────────────────
if [[ "$VERSION" == "latest" ]]; then
  VERSION=$(curl -sSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
            grep '"tag_name"' | cut -d'"' -f4)
fi

ARCHIVE="orkestra-agent_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${ARCHIVE}"
CHECKSUM_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/checksums.txt"

TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

echo "Downloading ${URL}"
curl -sSL -o "${TMP}/${ARCHIVE}" "$URL"

echo "Verifying checksum..."
curl -sSL -o "${TMP}/checksums.txt" "$CHECKSUM_URL"
(cd "$TMP" && grep "$ARCHIVE" checksums.txt | sha256sum --check)

tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
install -m 755 "${TMP}/orkestra-agent" "$BINARY_PATH"
echo "Binary installed at $BINARY_PATH"

# ─── Create directories & user ────────────────────────────────────────────────
useradd --system --no-create-home --shell /usr/sbin/nologin orkestra 2>/dev/null || true
mkdir -p "$DATA_DIR"
chown -R orkestra:orkestra "$DATA_DIR"
chmod 700 "$DATA_DIR"

# ─── Enroll ───────────────────────────────────────────────────────────────────
echo "Enrolling agent with master at ${MASTER}..."
"$BINARY_PATH" enroll \
  --master "$MASTER" \
  --bootstrap-token "$TOKEN" \
  --name "$NAME" \
  --data-dir "$DATA_DIR"

# ─── Install systemd service ──────────────────────────────────────────────────
# Write env file
mkdir -p /etc/orkestra/agent
cat > /etc/orkestra/agent/env <<EOF
ORKESTRA_MASTER_ADDR=${MASTER}
ORKESTRA_AGENT_DATA=${DATA_DIR}
ORKESTRA_LOG_LEVEL=info
EOF
chmod 600 /etc/orkestra/agent/env

# Install service file (bundled in archive or download)
if [[ -f "${TMP}/orkestra-agent.service" ]]; then
  install -m 644 "${TMP}/orkestra-agent.service" /etc/systemd/system/orkestra-agent.service
fi

# Check if docker group exists and prefer non-root
if getent group docker > /dev/null 2>&1; then
  usermod -aG docker orkestra 2>/dev/null || true
  sed -i 's/^User=root/User=orkestra/' /etc/systemd/system/orkestra-agent.service 2>/dev/null || true
  echo "Using orkestra user with docker group access"
else
  echo "Warning: docker group not found — running as root"
fi

systemctl daemon-reload
systemctl enable orkestra-agent
systemctl start orkestra-agent

echo ""
echo "✓ orkestra-agent installed and running"
echo "  Status:  systemctl status orkestra-agent"
echo "  Logs:    journalctl -u orkestra-agent -f"
