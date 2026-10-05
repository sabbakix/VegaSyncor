#!/bin/sh
# Installs VegaSyncor without the .deb package.
# Usage: sudo ./install.sh [binary-path]   (default: bin/vegasyncor)
set -e
BIN=${1:-bin/vegasyncor}
HERE=$(cd "$(dirname "$0")" && pwd)

if [ "$(id -u)" -ne 0 ]; then
    echo "Run as root: sudo $0" >&2
    exit 1
fi
if [ ! -x "$BIN" ]; then
    echo "Binary $BIN not found: run 'make build' first" >&2
    exit 1
fi

echo "→ installing dependencies (rsync, cifs-utils, smbclient, nftables)"
if command -v apt-get >/dev/null; then
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq rsync cifs-utils smbclient nftables
else
    echo "  apt-get not available: install rsync, cifs-utils, smbclient and nftables manually"
fi

echo "→ copying the program to /usr/bin/vegasyncor"
install -m 755 "$BIN" /usr/bin/vegasyncor
install -D -m 644 "$HERE/packaging/vegasyncor.service" /lib/systemd/system/vegasyncor.service

getent group vegasyncor >/dev/null || addgroup --system vegasyncor >/dev/null

echo "→ starting the service"
systemctl daemon-reload
systemctl enable --now vegasyncor.service
systemctl restart vegasyncor.service

# check that the environment can mount SMB shares (e.g. unprivileged container)
if ! CHECK=$(/usr/bin/vegasyncor check 2>&1); then
    echo
    echo "############################################################"
    printf '%s\n' "$CHECK"
    echo "############################################################"
fi

cat <<MSG

VegaSyncor installed and started.

  Management:   sudo vegasyncor
  Quick status: sudo vegasyncor status
  Service log:  journalctl -u vegasyncor -f

IMPORTANT: keep a copy of /etc/vegasyncor/master.key in a safe place:
without this key the saved passwords can no longer be read.
MSG
