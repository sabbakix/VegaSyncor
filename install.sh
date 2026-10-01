#!/bin/sh
# Installazione di VegaSyncor senza pacchetto .deb.
# Uso: sudo ./install.sh [percorso-binario]   (default: bin/vegasyncor)
set -e
BIN=${1:-bin/vegasyncor}
HERE=$(cd "$(dirname "$0")" && pwd)

if [ "$(id -u)" -ne 0 ]; then
    echo "Eseguire come root: sudo $0" >&2
    exit 1
fi
if [ ! -x "$BIN" ]; then
    echo "Binario $BIN non trovato: eseguire prima 'make build'" >&2
    exit 1
fi

echo "→ installazione dipendenze (rsync, cifs-utils, smbclient)"
if command -v apt-get >/dev/null; then
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq rsync cifs-utils smbclient
else
    echo "  apt-get non disponibile: installare a mano rsync, cifs-utils e smbclient"
fi

echo "→ copia del programma in /usr/bin/vegasyncor"
install -m 755 "$BIN" /usr/bin/vegasyncor
install -D -m 644 "$HERE/packaging/vegasyncor.service" /lib/systemd/system/vegasyncor.service

getent group vegasyncor >/dev/null || addgroup --system vegasyncor >/dev/null

echo "→ avvio del servizio"
systemctl daemon-reload
systemctl enable --now vegasyncor.service
systemctl restart vegasyncor.service

cat <<MSG

VegaSyncor installato e avviato.

  Gestione:          sudo vegasyncor
  Stato rapido:      sudo vegasyncor status
  Log del servizio:  journalctl -u vegasyncor -f

IMPORTANTE: fare una copia di /etc/vegasyncor/master.key in un luogo sicuro:
senza questa chiave le password salvate non sono più leggibili.
MSG
