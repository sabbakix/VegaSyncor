#!/bin/bash
# Builds VegaSyncor, creates the demo data (simulated SMB shares and OneDrive)
# and starts the service in the tmux session "d".
set -e
. "$(dirname "$0")/env.sh"
cd "$REPO"
PATH=$HOME/.local/go/bin:$PATH go build -o bin/vegasyncor ./cmd/vegasyncor
tmux kill-session -t d 2>/dev/null || true
pkill -x vegasyncor 2>/dev/null || true
sleep 0.5
rm -rf "$DEMO"
mkdir -p "$DEMO"/{etc,state,run,onedrive}
R=$DEMO/smb
cat > "$DEMO/env.sh" <<ENV
export VEGASYNCOR_DEV=1 VEGASYNCOR_DEV_SMB_ROOT=$R VEGASYNCOR_DEV_ONEDRIVE_ROOT=$DEMO/onedrive \
  VEGASYNCOR_CONFIG_DIR=$DEMO/etc VEGASYNCOR_STATE_DIR=$DEMO/state VEGASYNCOR_RUNTIME_DIR=$DEMO/run \
  VEGASYNCOR_SOCKET=$DEMO/run/vegasyncor.sock VEGASYNCOR_LANG=en TZ=Europe/London \
  TERM=xterm-256color COLORTERM=truecolor
ENV
. "$DEMO/env.sh"

mk() { mkdir -p "$(dirname "$1")"; head -c "${2:-20000}" /dev/urandom > "$1"; }
A="$R/ADMIN-PC/Documents/Accounting"
for m in 01 02 03 04 05 06 07 08 09; do for n in 1 2 3 4; do mk "$A/2026/$m/Invoice_2026-$m-00$n.pdf" 80000; done; done
mk "$A/Balance sheet 2025.xlsx" 300000; mk "$A/Ledger.xlsx" 120000; mk "$A/Taxes/VAT_September.pdf" 60000
mkdir -p "$R/ADMIN-PC/Scans" "$R/ADMIN-PC/Documents/"{HR,Suppliers,Contracts,"Quotes 2026"}
P="$R/FILESRV01/Projects"
for c in "North Warehouse" "Main Street Site" "Riverside Villa" "City School"; do
  for i in $(seq 1 30); do mk "$P/$c/Drawing_$i.dwg" 1000000; done
done
mkdir -p "$R/FILESRV01/Common" "$R/FILESRV01/Software"
for i in $(seq 1 25); do mk "$R/ENG-PC/Drawings/Detail_$i.dwg" 150000; done
mkdir -p "$R/SAMBA-SRV/home/mario" "$R/NAS-BACKUP/Backup" "$R/NAS-BACKUP/Archive"
mkdir -p "$DEMO/onedrive/dev-drive/"{Documents,Pictures,"VegaSyncor/Engineering"}

tmux new-session -d -s d "bash -c '. $DEMO/env.sh; ./bin/vegasyncor daemon 2>$DEMO/daemon.log'"
sleep 1
api() { curl -s --unix-socket "$VEGASYNCOR_SOCKET" -X "$1" "http://x$2" -d "$3"; }
id() { python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id") or d)'; }
C1=$(api POST /api/connections '{"name":"Admin office PC","host":"ADMIN-PC","domain":"COMPANY","username":"backup","password":"x1!Demo"}' | id)
C2=$(api POST /api/connections '{"name":"File server","host":"FILESRV01","domain":"COMPANY","username":"svc-backup","password":"x2!Demo"}' | id)
C3=$(api POST /api/connections '{"name":"Engineering PC","host":"ENG-PC","username":"backup","password":"x3!Demo","smb_version":"2.1"}' | id)
C4=$(api POST /api/connections '{"name":"Samba server","host":"SAMBA-SRV","username":"backup","password":"x4!Demo","smb_version":"3.1.1"}' | id)
C5=$(api POST /api/connections '{"name":"Backup NAS","host":"NAS-BACKUP","username":"vegasyncor","password":"x5!Demo"}' | id)
# OneDrive: simulated sign-in
api POST /api/onedrive/finish '{"session":"dev","redirect":"http://localhost:53682/?code=demo&state=dev"}' > "$DEMO/acc.json"
OD=$(python3 - "$DEMO/acc.json" <<'PY' | curl -s --unix-socket "$VEGASYNCOR_SOCKET" -X POST http://x/api/connections -d @- | id
import json, sys
a = json.load(open(sys.argv[1]))
print(json.dumps({"name": "Off-site OneDrive", "kind": "onedrive", "account": a["account"], "drive_id": a["drive_id"],
                  "drive_type": a["drive_type"], "token": a["token"]}))
PY
)
smb() { echo "{\"type\":\"smb\",\"connection_id\":\"$1\",\"share\":\"$2\",\"path\":\"$3\"}"; }
job() { api POST /api/jobs "$1" | id; }
job "{\"name\":\"Accounting\",\"enabled\":true,\"source\":$(smb $C1 Documents Accounting),\"source_readonly\":true,\"dest\":$(smb $C5 Backup Accounting),\"mode\":\"mirror_archive\",\"archive_days\":30,\"schedule\":{\"type\":\"weekly\",\"times\":[\"13:00\",\"22:00\"],\"days\":[1,2,3,4,5]}}" >/dev/null
job "{\"name\":\"Accounting off-site\",\"enabled\":true,\"source\":$(smb $C1 Documents Accounting),\"source_readonly\":true,\"dest\":{\"type\":\"onedrive\",\"connection_id\":\"$OD\",\"path\":\"VegaSyncor/Accounting\"},\"mode\":\"mirror_archive\",\"archive_days\":90,\"encrypt\":true,\"encrypt_names\":true,\"crypt_password\":\"demo-password\",\"bandwidth_kbps\":2000,\"schedule\":{\"type\":\"daily\",\"times\":[\"02:00\"]}}" >/dev/null
job "{\"name\":\"CAD projects\",\"enabled\":true,\"source\":$(smb $C2 Projects ""),\"source_readonly\":true,\"dest\":$(smb $C5 Backup Projects),\"mode\":\"mirror_archive\",\"archive_days\":60,\"bandwidth_kbps\":1200,\"excludes\":[\"*.bak\",\"*.dwl\",\"*.dwl2\"],\"schedule\":{\"type\":\"interval\",\"every_minutes\":120,\"window_from\":\"07:00\",\"window_to\":\"20:00\",\"days\":[1,2,3,4,5]}}" > "$DEMO/cad.id"
job "{\"name\":\"Engineering\",\"enabled\":true,\"source\":$(smb $C3 Drawings ""),\"source_readonly\":true,\"dest\":$(smb $C5 Backup Engineering),\"mode\":\"additive\",\"schedule\":{\"type\":\"daily\",\"times\":[\"23:00\"]}}" >/dev/null
job "{\"name\":\"Samba home folders\",\"enabled\":true,\"source\":$(smb $C4 home users),\"source_readonly\":true,\"dest\":$(smb $C5 Backup samba-home),\"mode\":\"mirror\",\"schedule\":{\"type\":\"daily\",\"times\":[\"01:30\"]}}" >/dev/null
job "{\"name\":\"Scans\",\"enabled\":false,\"source\":$(smb $C1 Scans ""),\"source_readonly\":true,\"dest\":$(smb $C5 Archive Scans),\"mode\":\"additive\",\"allow_empty_source\":true,\"schedule\":{\"type\":\"cron\",\"cron\":\"0 */4 * * 1-5\"}}" >/dev/null

for j in Accounting "Accounting off-site" Engineering "Samba home folders" Scans; do ./bin/vegasyncor run "$j" | tail -1; done
# changes since the last copy, for the dry run
head -c 95000 /dev/urandom > "$A/2026/09/Invoice_2026-09-005.pdf"; head -c 91000 /dev/urandom > "$A/2026/09/Invoice_2026-09-006.pdf"
rm -f "$A/2026/01/Invoice_2026-01-004.pdf"; head -c 131000 /dev/urandom > "$A/Ledger.xlsx"
./bin/vegasyncor dry-run Accounting | tail -1
echo "demo ready: . $DEMO/env.sh"
