#!/bin/bash
# Creates the Playwright venv and extracts the libraries headless Chromium needs
# (no root: the .deb packages are only unpacked).
set -e
. "$(dirname "$0")/env.sh"
mkdir -p "$SHOTS_DIR"
cd "$SHOTS_DIR"
[ -x venv/bin/python ] || python3 -m venv venv
venv/bin/pip install -q playwright
venv/bin/python -m playwright install chromium-headless-shell >/dev/null
mkdir -p libs/debs
cd libs/debs
for p in libnspr4 libnss3 libasound2t64; do
  # the candidate can be missing from the mirrors when the package index is old:
  # fall back to the version of the release
  apt-get download "$p" >/dev/null 2>&1 ||
    apt-get download "$p=$(apt-cache madison "$p" | tail -1 | awk -F'|' '{gsub(/ /, "", $2); print $2}')" >/dev/null
done
for d in *.deb; do dpkg-deb -x "$d" ../root; done
echo "ready in $SHOTS_DIR"
