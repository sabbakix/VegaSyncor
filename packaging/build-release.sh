#!/bin/sh
# Prepara in dist/release tutti i file di una release GitHub:
# pacchetti .deb, tarball con binario statico, script di installazione, checksum e note.
# Uso: packaging/build-release.sh <versione>     es. packaging/build-release.sh 0.2.0
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?specificare la versione (es. 0.2.0)}
VERSION=${VERSION#v}
ARCHS="amd64 arm64"
OUT=dist/release

rm -rf dist
packaging/build-deb.sh "$VERSION" $ARCHS
mkdir -p "$OUT"
mv dist/vegasyncor_"$VERSION"_*.deb "$OUT/"

# tarball: binario + unità systemd + install.sh per sistemi senza apt
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

# note di rilascio: elenco dei commit dal tag precedente
REF=HEAD
git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null && REF=v$VERSION
# per le versioni stabili il confronto è con l'ultima stabile (le pre-release vengono ignorate)
case "$VERSION" in
*-*) PREV=$(git describe --tags --abbrev=0 --match 'v*' "$REF^" 2>/dev/null || true) ;;
*) PREV=$(git describe --tags --abbrev=0 --match 'v*' --exclude 'v*-*' "$REF^" 2>/dev/null || true) ;;
esac
{
    case "$VERSION" in
    *-*)
        echo "> 🧪 **Versione di prova** (pre-release): non viene installata dal comando rapido. Per installarla:"
        echo '> ```bash'
        echo "> curl -fsSL https://github.com/sabbakix/VegaSyncor/releases/latest/download/vegasyncor-install.sh | sudo VEGASYNCOR_VERSION=$VERSION sh"
        echo '> ```'
        echo
        ;;
    esac
    if [ -n "$PREV" ]; then
        echo "## Novità rispetto a $PREV"
        echo
        git log --no-merges --format='- %s' "$PREV..$REF"
    else
        echo "## Prima versione"
    fi
} > dist/changes.md
awk -v f=dist/changes.md '
    /\{\{CHANGES\}\}/ { while ((getline l < f) > 0) print l; next }
    { gsub(/\{\{VERSION\}\}/, ver); print }
' ver="$VERSION" packaging/release-notes.md > "$OUT/NOTES.md"
rm dist/changes.md

echo "→ release $VERSION pronta in $OUT:"
ls -1 "$OUT"
