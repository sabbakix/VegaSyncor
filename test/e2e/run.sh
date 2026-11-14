#!/bin/bash
# Runs the end-to-end tests: builds the .deb, installs it in a dedicated LXD
# container and runs e2e.sh there against the SMB server configured in .env.
#
#   test/e2e/run.sh              # container "vs-test" (created if missing)
#   E2E_CONTAINER=other test/e2e/run.sh
#
# The container is privileged (needed for CIFS mounts) and is wiped of any
# VegaSyncor configuration on every run: only containers created by this
# script (marked with user.vegasyncor-e2e=true) are used.
set -euo pipefail
cd "$(dirname "$0")/../.."
CT=${E2E_CONTAINER:-vs-test}
IMAGE=${E2E_IMAGE:-ubuntu:26.04}

[ -r .env ] || { echo ".env not found: copy .env.example to .env and fill it in" >&2; exit 1; }
if [ -n "$(find .env -perm /077)" ]; then
  echo "warning: .env is readable by other users, fixing it (chmod 600)" >&2
  chmod 600 .env
fi
set -a
# shellcheck disable=SC1091 # .env is local and git-ignored
. ./.env
set +a
: "${SMB_HOST:?missing in .env}" "${SMB_USER:?missing in .env}" "${SMB_PASS:?missing in .env}"

if ! lxc info "$CT" >/dev/null 2>&1; then
  echo "→ creating the test container $CT ($IMAGE)"
  lxc launch "$IMAGE" "$CT" -c security.privileged=true -c security.nesting=true \
    -c limits.memory=2GiB -c limits.cpu=2 -c user.vegasyncor-e2e=true
  for _ in $(seq 60); do
    lxc exec "$CT" -- systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded' && break
    sleep 2
  done
  lxc exec "$CT" -- bash -c 'DEBIAN_FRONTEND=noninteractive apt-get update -qq &&
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq smbclient nftables rclone curl python3 >/dev/null'
fi
if [ "$(lxc config get "$CT" user.vegasyncor-e2e)" != true ]; then
  echo "$CT is not an e2e test container (user.vegasyncor-e2e is not true): refusing to wipe it" >&2
  exit 1
fi

echo "→ building the package"
rm -f dist/vegasyncor_*_amd64.deb
make deb ARCHS=amd64 >/dev/null
DEB=$(ls dist/vegasyncor_*_amd64.deb)
lxc file push "$DEB" "$CT/root/vegasyncor.deb"
lxc file push --mode 0700 test/e2e/e2e.sh "$CT/root/e2e.sh"

echo "→ installing $(basename "$DEB") in $CT (clean configuration)"
lxc exec "$CT" -- bash -c 'DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --reinstall /root/vegasyncor.deb >/dev/null 2>&1 &&
  systemctl stop vegasyncor && rm -rf /etc/vegasyncor/config.json /var/lib/vegasyncor/* &&
  systemctl start vegasyncor && vegasyncor version'

# credentials: a 0600 file in the container, only for the duration of the run
ENVF=$(mktemp)
trap 'rm -f "$ENVF"; lxc exec "$CT" -- rm -f /root/e2e.env' EXIT
for k in SMB_HOST SMB_USER SMB_PASS SMB_SHARE_A SMB_SHARE_B; do
  [ -n "${!k:-}" ] && printf 'export %s=%q\n' "$k" "${!k}"
done > "$ENVF"
lxc file push --mode 0600 "$ENVF" "$CT/root/e2e.env"

echo "→ running the tests against //$SMB_HOST"
lxc exec "$CT" -- bash -c '. /root/e2e.env && /root/e2e.sh'
