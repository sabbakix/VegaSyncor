#!/bin/sh
# Installa (o aggiorna) VegaSyncor dall'ultima release GitHub su Debian/Ubuntu.
# Uso:  curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo sh
# Versione specifica:  ... | sudo VEGASYNCOR_VERSION=0.1.0 sh
set -eu
REPO="sabbakix/VegaSyncor"

if [ "$(id -u)" -ne 0 ]; then
    echo "Eseguire come root (sudo)." >&2
    exit 1
fi
if ! command -v apt-get >/dev/null 2>&1; then
    echo "Questo script supporta solo Debian/Ubuntu (apt)." >&2
    exit 1
fi

case "$(dpkg --print-architecture)" in
    amd64) ARCH=amd64 ;;
    arm64) ARCH=arm64 ;;
    *) echo "Architettura $(dpkg --print-architecture) non supportata (solo amd64, arm64)." >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y -qq curl ca-certificates; }

if [ -n "${VEGASYNCOR_VERSION:-}" ]; then
    VERSION=${VEGASYNCOR_VERSION#v}
else
    # la pagina /releases/latest reindirizza a /releases/tag/vX.Y.Z
    VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's|.*/tag/v\{0,1\}||')
fi
[ -n "$VERSION" ] || { echo "Impossibile determinare l'ultima versione." >&2; exit 1; }

BASE="https://github.com/$REPO/releases/download/v$VERSION"
DEB="vegasyncor_${VERSION}_${ARCH}.deb"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "→ download VegaSyncor $VERSION ($ARCH)"
curl -fsSL -o "$TMP/$DEB" "$BASE/$DEB"
curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"

echo "→ verifica checksum"
(cd "$TMP" && grep " $DEB\$" SHA256SUMS | sha256sum -c -)

echo "→ installazione (con dipendenze rsync, cifs-utils, smbclient)"
chmod 644 "$TMP/$DEB"
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y "$TMP/$DEB" smbclient

echo
systemctl --no-pager --lines=0 status vegasyncor || true

# verifica che l'ambiente possa montare le condivisioni SMB (es. container non privilegiato)
if ! CHECK=$(/usr/bin/vegasyncor check 2>&1); then
    echo
    echo "############################################################"
    printf '%s\n' "$CHECK"
    echo "############################################################"
fi
cat <<MSG

VegaSyncor $VERSION installato.

  Gestione:          sudo vegasyncor
  Stato rapido:      sudo vegasyncor status
  Log del servizio:  journalctl -u vegasyncor -f

IMPORTANTE: salvare una copia di /etc/vegasyncor/master.key in un luogo sicuro.
MSG
