#!/bin/sh
# postremove for orkestra-agent (deb: "remove"/"purge"/"upgrade", rpm: 0/1)
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# Only wipe agent config/identity on an explicit purge (deb). Keeps the cert
# across a plain remove/upgrade so re-installing does not force a re-enroll.
if [ "$1" = "purge" ]; then
    rm -rf /etc/orkestra/agent
fi

exit 0
