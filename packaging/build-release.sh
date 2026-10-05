#!/bin/sh
# Prepares in dist/release all the files of a GitHub release:
# .deb packages, tarballs with the static binary, install script, checksums and notes.
# Usage: packaging/build-release.sh <version>     e.g. packaging/build-release.sh 0.3.0
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?specify the version (e.g. 0.3.0)}
VERSION=${VERSION#v}
ARCHS="amd64 arm64"
OUT=dist/release

rm -rf dist
packaging/build-deb.sh "$VERSION" $ARCHS
mkdir -p "$OUT"
mv dist/vegasyncor_"$VERSION"_*.deb "$OUT/"

# tarball: binary + systemd unit + install.sh for systems without apt
for a in $ARCHS; do
    name=vegasyncor_${VERSION}_linux_$a
    stage=dist/$name
    install -D -m755 dist/vegasyncor-linux-$a "$stage/vegasyncor"
    install -D -m755 install.sh "$stage/install.sh"
    install -D -m644 packaging/vegasyncor.service "$stage/packaging/vegasyncor.service"
    install -D -m644 README.md "$stage/README.md"
    tar -C dist -czf "$OUT/$name.tar.gz" --owner=0 --group=0 "$name"
    rm -rf "$stage"
done

install -m755 packaging/vegasyncor-install.sh "$OUT/vegasyncor-install.sh"
(cd "$OUT" && sha256sum *.deb *.tar.gz vegasyncor-install.sh > SHA256SUMS)

# release notes: list of commits since the previous tag
REF=HEAD
git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null && REF=v$VERSION
# stable versions are compared with the last stable one (pre-releases are ignored)
case "$VERSION" in
*-*) PREV=$(git describe --tags --abbrev=0 --match 'v*' "$REF^" 2>/dev/null || true) ;;
*) PREV=$(git describe --tags --abbrev=0 --match 'v*' --exclude 'v*-*' "$REF^" 2>/dev/null || true) ;;
esac
{
    case "$VERSION" in
    *-*)
        echo "> 🧪 **Test version** (pre-release): it is not installed by the quick install command. To install it:"
        echo '> ```bash'
        echo "> curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo VEGASYNCOR_VERSION=$VERSION sh"
        echo '> ```'
        echo
        ;;
    esac
    if [ -n "$PREV" ]; then
        echo "## Changes since $PREV"
        echo
        git log --no-merges --format='- %s' "$PREV..$REF"
    else
        echo "## First release"
    fi
} > dist/changes.md
awk -v f=dist/changes.md '
    /\{\{CHANGES\}\}/ { while ((getline l < f) > 0) print l; next }
    { gsub(/\{\{VERSION\}\}/, ver); print }
' ver="$VERSION" packaging/release-notes.md > "$OUT/NOTES.md"
rm dist/changes.md

echo "→ release $VERSION ready in $OUT:"
ls -1 "$OUT"
