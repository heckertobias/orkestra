#!/bin/sh
# postinstall for orkestra-master (deb: "configure", rpm: 1=install 2=upgrade)
set -e

# The master unit runs as the unprivileged "orkestra" user.
if ! getent passwd orkestra >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin orkestra 2>/dev/null \
        || adduser --system --no-create-home --shell /usr/sbin/nologin orkestra 2>/dev/null \
        || true
fi

mkdir -p /etc/orkestra/master /var/lib/orkestra
chown -R orkestra:orkestra /etc/orkestra/master /var/lib/orkestra 2>/dev/null || true
chmod 750 /etc/orkestra/master
[ -f /etc/orkestra/master/env ] && chmod 640 /etc/orkestra/master/env || true
[ -f /etc/orkestra/master/env ] && chown orkestra:orkestra /etc/orkestra/master/env 2>/dev/null || true

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable orkestra-master.service || true
fi

if [ "$1" = "configure" ] || [ "$1" = "1" ]; then
    cat <<'EOF'

orkestra-master installed. Before the first start:

  1. Point ORKESTRA_DATABASE_URL in /etc/orkestra/master/env at a PostgreSQL DB.
  2. Create the KEK (kept separate from the DB credentials):
       openssl rand -hex 32 | sudo tee /etc/orkestra/master/master.key >/dev/null
       sudo chmod 600 /etc/orkestra/master/master.key
       sudo chown orkestra:orkestra /etc/orkestra/master/master.key
  3. Start it:
       sudo systemctl enable --now orkestra-master
       # then open the one-time setup URL printed in: journalctl -u orkestra-master

Note: for most setups running the Master via Docker/Compose is simpler.
EOF
fi

exit 0
