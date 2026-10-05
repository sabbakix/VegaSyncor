#!/bin/sh
# Installs (or updates) VegaSyncor from the latest GitHub release on Debian/Ubuntu.
# Usage:  curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh
# Specific version:  ... | sudo VEGASYNCOR_VERSION=0.1.0 sh
set -eu
REPO="sabbakix/VegaSyncor"

if [ "$(id -u)" -ne 0 ]; then
    echo "Run as root (sudo)." >&2
    exit 1
fi
if ! command -v apt-get >/dev/null 2>&1; then
    echo "This script only supports Debian/Ubuntu (apt)." >&2
    exit 1
fi

case "$(dpkg --print-architecture)" in
    amd64) ARCH=amd64 ;;
    arm64) ARCH=arm64 ;;
    *) echo "Architecture $(dpkg --print-architecture) not supported (amd64 and arm64 only)." >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y -qq curl ca-certificates; }

if [ -n "${VEGASYNCOR_VERSION:-}" ]; then
    VERSION=${VEGASYNCOR_VERSION#v}
else
    # the /releases/latest page redirects to /releases/tag/vX.Y.Z
    VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's|.*/tag/v\{0,1\}||')
fi
[ -n "$VERSION" ] || { echo "Cannot determine the latest version." >&2; exit 1; }

BASE="https://github.com/$REPO/releases/download/v$VERSION"
DEB="vegasyncor_${VERSION}_${ARCH}.deb"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "→ download VegaSyncor $VERSION ($ARCH)"
curl -fsSL -o "$TMP/$DEB" "$BASE/$DEB"
curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"

echo "→ verifying checksum"
(cd "$TMP" && grep " $DEB\$" SHA256SUMS | sha256sum -c -)

echo "→ installing (with dependencies rsync, cifs-utils, smbclient)"
chmod 644 "$TMP/$DEB"
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y "$TMP/$DEB" smbclient

echo
systemctl --no-pager --lines=0 status vegasyncor || true

# check that the environment can mount SMB shares (e.g. unprivileged container)
if ! CHECK=$(/usr/bin/vegasyncor check 2>&1); then
    echo
    echo "############################################################"
    printf '%s\n' "$CHECK"
    echo "############################################################"
fi
cat <<MSG

VegaSyncor $VERSION installed.

  Management:   sudo vegasyncor
  Quick status: sudo vegasyncor status
  Service log:  journalctl -u vegasyncor -f

IMPORTANT: keep a copy of /etc/vegasyncor/master.key in a safe place.
MSG
