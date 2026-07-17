#!/bin/sh
# preremove for orkestra-agent (deb: "remove"/"upgrade", rpm: 0=uninstall 1=upgrade)
set -e

# Stop/disable only on a real removal, not during an upgrade.
if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop orkestra-agent.service || true
        systemctl disable orkestra-agent.service || true
    fi
fi

exit 0
