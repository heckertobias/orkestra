#!/bin/sh
# preremove for orkestra-master (deb: "remove"/"upgrade", rpm: 0=uninstall 1=upgrade)
set -e

if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
    if command -v systemctl >/dev/null 2>&1; then
        systemctl stop orkestra-master.service || true
        systemctl disable orkestra-master.service || true
    fi
fi

exit 0
