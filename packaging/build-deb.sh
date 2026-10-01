#!/bin/sh
# Compila i binari statici e crea i pacchetti .deb.
# Uso: packaging/build-deb.sh <versione> [architetture...]   es. packaging/build-deb.sh 0.1.0 amd64 arm64
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?specificare la versione}; shift
ARCHS=${*:-amd64 arm64}
DEB_VERSION=$(echo "$VERSION" | sed -E 's/^v//; s/-/+/g; s/^([^0-9])/0.0.0+\1/')
GO=${GO:-go}
mkdir -p dist
for a in $ARCHS; do
    CGO_ENABLED=0 GOOS=linux GOARCH=$a $GO build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
        -o dist/vegasyncor-linux-$a ./cmd/vegasyncor
    R=dist/deb-$a
    rm -rf "$R"
    install -D -m755 dist/vegasyncor-linux-$a "$R/usr/bin/vegasyncor"
    install -D -m644 packaging/vegasyncor.service "$R/lib/systemd/system/vegasyncor.service"
    install -D -m644 README.md "$R/usr/share/doc/vegasyncor/README.md"
    install -d "$R/DEBIAN"
    install -m755 packaging/postinst packaging/prerm packaging/postrm "$R/DEBIAN/"
    SIZE=$(du -sk "$R" | cut -f1)
    cat > "$R/DEBIAN/control" <<CTRL
Package: vegasyncor
Version: $DEB_VERSION
Section: admin
Priority: optional
Architecture: $a
Installed-Size: $SIZE
Depends: rsync, cifs-utils
Recommends: smbclient
Maintainer: VegaSyncor <root@localhost>
Description: sincronizzazione pianificata di cartelle di rete per backup
 Servizio e interfaccia testuale (TUI) per copiare periodicamente
 cartelle condivise SMB/CIFS (Windows, Samba) su un server di backup.
CTRL
    dpkg-deb --root-owner-group --build "$R" "dist/vegasyncor_${DEB_VERSION}_$a.deb" >/dev/null
    rm -rf "$R"
    echo "→ dist/vegasyncor_${DEB_VERSION}_$a.deb"
done
