#!/bin/bash
# Takes the README screenshots from the demo started by demo.sh (07-firewall is
# not included: it needs nftables in a network namespace).
set -e
. "$(dirname "$0")/env.sh"
. "$DEMO/env.sh"
cd "$REPO"
start() { # start WIDTH HEIGHT
  tmux kill-session -t t 2>/dev/null || true
  tmux new-session -d -s t -x "$1" -y "$2" "bash -c '. $DEMO/env.sh; ./bin/vegasyncor'"
  sleep 1.5
}
k() { for x in "$@"; do tmux send-keys -t t "$x"; sleep 0.2; done; }
typ() { tmux send-keys -t t -l "$1"; sleep 0.2; }
shot() { "$TOOLS/shot.sh" "$@" >/dev/null && echo "ok $1"; }
# go_to LABEL: moves the form cursor to the field with that label
go_to() {
  for _ in $(seq 1 60); do
    tmux capture-pane -t t -p | grep -qE "^ >[ *]*$1 " && return
    k Down
  done
  echo "field not found: $1" >&2
}
pick() { # pick LABEL TEXT: changes a choice until it shows TEXT
  for _ in 1 2 3 4; do
    tmux capture-pane -t t -p | grep -E "^ >[ *]*$1 " | grep -q "$2" && return
    k Right
  done
  echo "choice not found: $2" >&2
}

rm -f "$OUT"/*.png

# 01: dashboard with a copy in progress
rm -rf "$VEGASYNCOR_DEV_SMB_ROOT/NAS-BACKUP/Backup/Projects"
curl -s --unix-socket "$VEGASYNCOR_SOCKET" -X POST "http://x/api/jobs/$(cat "$DEMO/cad.id")/run" >/dev/null
start 118 32; k Down Down; sleep 20; shot 01-dashboard

# 02: editing a job, with changed fields
start 118 47; k Enter
go_to "Keep archive \(days\)"; k End BSpace BSpace; typ 60
go_to "Exclude"; typ '*.tmp, ~$*'
go_to "Times"; tmux send-keys -t t C-u; typ "08:00, 13:00, 22:00"; sleep 0.3
shot 02-edit-job

# 03: choosing a destination folder, with a new folder
start 118 26; k Enter
go_to "Subfolder"; k Down; go_to "Subfolder"; k Enter; sleep 1; k n; typ 2027; sleep 0.3
shot 03-browse-folders --trim-top

# 04: connections, including OneDrive
start 118 20; k 2; for _ in 1 2 3 4 5 6; do k Down; done; sleep 0.3; shot 04-connections

# 05: dry run log
start 118 34; k 3; sleep 1.2
for _ in $(seq 1 12); do tmux capture-pane -t t -p | grep -q '^ > .*Accounting (sim)' && break; k Down; done
k Enter; sleep 1; k g; sleep 0.5; shot 05-dry-run

# 06: history (after the copy in progress has finished)
for _ in $(seq 1 60); do ./bin/vegasyncor status | grep -q "CAD projects.*running" || break; sleep 2; done
start 118 20; k 3; sleep 1.5; shot 06-history

# 08: export
start 118 18; k 2 E; tmux send-keys -t t C-u; typ "/root/vegasyncor-backup01-20261031-1030.vsconf"
k Down; typ "correct-horse-battery"; k Down; typ "correct-horse-battery"; k Up; sleep 0.3
shot 08-export

# 09: OneDrive sign-in, with the address pasted back
start 118 30; k 2 n; go_to "Type"; pick "Type" "OneDrive"; go_to "Account"; k Enter; sleep 1
typ "http://localhost:53682/?code=M.C540_BAY.2.U.d1f5a0c2-7e1b-4a8c-9e3f-0b6d5c4a3e21&state=pX2kq9"; sleep 0.3
shot 09-onedrive-signin

# 10: OneDrive destination with encryption
start 118 50; for _ in 1; do k Down; done; k Enter
go_to "Encrypted copy"; sleep 0.3
shot 10-onedrive-job

tmux kill-session -t t
ls "$OUT"/*.png
