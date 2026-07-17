#!/bin/sh
# postremove for orkestra-master (deb: "remove"/"purge"/"upgrade", rpm: 0/1)
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# Purge removes config incl. the KEK and local state. This is destructive —
# a lost KEK makes encrypted DB data unrecoverable. Only on explicit purge.
if [ "$1" = "purge" ]; then
    rm -rf /etc/orkestra/master /var/lib/orkestra
fi

exit 0
