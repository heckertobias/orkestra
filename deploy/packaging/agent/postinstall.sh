#!/bin/sh
# postinstall for orkestra-agent (deb: "configure", rpm: 1=install 2=upgrade)
set -e

# The agent needs /var/run/docker.sock, so the unit runs as root by default.
# Data (cert/key/config) live in /etc/orkestra/agent.
mkdir -p /etc/orkestra/agent /var/lib/orkestra
chmod 700 /etc/orkestra/agent
[ -f /etc/orkestra/agent/env ] && chmod 600 /etc/orkestra/agent/env || true

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable orkestra-agent.service || true
fi

# Only print the getting-started hint on a fresh install, not on upgrade.
if [ "$1" = "configure" ] || [ "$1" = "1" ]; then
    cat <<'EOF'

orkestra-agent installed. Enroll it once, then start the service:

  sudo orkestra-agent enroll --master https://<master>:4440 \
       --bootstrap-token <token> --name <server-name>
  sudo systemctl enable --now orkestra-agent

Get a bootstrap token from the Master UI: Servers -> Add Server.
EOF
fi

exit 0
