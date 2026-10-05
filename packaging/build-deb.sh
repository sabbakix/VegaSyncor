#!/bin/sh
# Builds the static binaries and creates the .deb packages.
# Usage: packaging/build-deb.sh <version> [architectures...]   e.g. packaging/build-deb.sh 0.3.0 amd64 arm64
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?specify the version}; shift
VERSION=${VERSION#v}
ARCHS=${*:-amd64 arm64}
# Debian version: pre-releases (0.2.0-rc1) use "~" so they sort before 0.2.0,
# development builds (0.1.0-3-gabc123) use "+" so they sort after 0.1.0.
DEB_VERSION=$(echo "$VERSION" | sed -E 's/-(rc|beta|alpha)/~\1/; s/-/+/g; s/^([^0-9])/0.0.0+\1/')
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
Description: scheduled sync of network folders for backup
 Service and text-based interface (TUI) to periodically copy
 SMB/CIFS shared folders (Windows, Samba) to a backup server.
CTRL
    dpkg-deb --root-owner-group --build "$R" "dist/vegasyncor_${VERSION}_$a.deb" >/dev/null
    rm -rf "$R"
    echo "→ dist/vegasyncor_${VERSION}_$a.deb (Debian version $DEB_VERSION)"
done
