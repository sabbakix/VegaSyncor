#!/bin/bash
# End-to-end test of the installed VegaSyncor package against a real SMB server.
# Runs as root inside the test container (started by run.sh, which provides
# SMB_HOST, SMB_USER, SMB_PASS, SMB_SHARE_A and SMB_SHARE_B).
#
# Only vs-test* folders are touched on the shares: the fixtures are created in
# SHARE_A/vs-test* and removed at the end; the copies in SHARE_B/vs-test* are
# replaced on every run and left there for inspection.
set -u
: "${SMB_HOST:?}" "${SMB_USER:?}" "${SMB_PASS:?}"
H=$SMB_HOST SA=${SMB_SHARE_A:-share-a} SB=${SMB_SHARE_B:-share-b}
SOCK=/run/vegasyncor/vegasyncor.sock
# fixtures get this old modification time, so a copy that loses it is detected
OLD_TIME=2025:01:02-03:04:05 OLD_SHOWN="Jan +2 03:04:05( AM)? 2025" # as shown by allinfo
NFIX=3 # large fixture files (8 MB each)

WORK=$(mktemp -d)
AUTH=$WORK/auth
(umask 077; printf 'username=%s\npassword=%s\n' "$SMB_USER" "$SMB_PASS" > "$AUTH")
trap 'rm -rf "$WORK"' EXIT

api() { curl -s --unix-socket $SOCK -X "$1" "http://x$2" -d "${3:-}"; }
id() { python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id") or d)'; }
smb() { LC_ALL=C smbclient "//$H/$1" -A "$AUTH" -c "$2" 2>&1; }
# exists SHARE PATH: smbclient also prints the name in its errors, so look for NT_STATUS_
exists() { ! smb "$1" "ls $2" | grep -q NT_STATUS_; }
mtime() { smb "$1" "allinfo \"$2\"" | sed -n 's/^write_time:[[:space:]]*//p'; }
loc() { echo "{\"type\":\"smb\",\"connection_id\":\"$C\",\"share\":\"$1\",\"path\":\"$2\"}"; }
job() { # job NAME SOURCE DEST MODE
  api POST /api/jobs "{\"name\":\"$1\",\"enabled\":true,\"source\":$2,\"source_readonly\":true,\"dest\":$3,\"mode\":\"$4\",\"archive_days\":30,\"schedule\":{\"type\":\"manual\"}}" >/dev/null
}
runjob() { timeout 300 vegasyncor "${2:-run}" "$1" 2>&1 | tr '\r' '\n' | grep -E '^Result'; }

pass=0 failn=0
check() { # check DESCRIPTION SHELL-CONDITION
  if eval "$2"; then echo "PASS  $1"; pass=$((pass + 1)); else echo "FAIL  $1"; failn=$((failn + 1)); fi
}
info() { echo "      $*"; }

# --- fixtures
smb "$SB" 'deltree vs-test-from-a; deltree vs-test-guard' >/dev/null
smb "$SA" 'deltree vs-test; deltree vs-test-empty' >/dev/null
mkdir "$WORK/fix"
for i in $(seq $NFIX); do head -c 8000000 /dev/urandom > "$WORK/fix/data_$i.bin"; done
echo one > "$WORK/fix/one.txt"; echo two > "$WORK/fix/two.txt"
cmds="mkdir vs-test-empty; mkdir vs-test; cd vs-test; lcd $WORK/fix"
for f in "$WORK"/fix/*; do
  b=$(basename "$f"); cmds="$cmds; put $b; utimes $b -1 -1 $OLD_TIME -1"
done
smb "$SA" "$cmds" >/dev/null
NFILES=$((NFIX + 2))
check "fixtures created on $SA with the old modification time" \
  "mtime $SA vs-test/data_1.bin | grep -qE \"$OLD_SHOWN\""

# --- connection, browsing
C=$(api POST /api/connections "{\"name\":\"E2E\",\"host\":\"$H\",\"username\":\"$SMB_USER\",\"password\":\"$SMB_PASS\"}" | id)
check "connection test lists $SA and $SB" \
  "api POST /api/connections/$C/test | grep -q '\"$SA\"' && api POST /api/connections/$C/test | grep -q '\"$SB\"'"
check "browse $SA (real read-only mount) lists the vs-test folder" \
  "api POST /api/browse '{\"location\":$(loc "$SA" '')}' | grep -q '\"vs-test\"'"
check "no mount left after browse" "! grep -q ' cifs ' /proc/mounts"

# --- job 1: SA/vs-test -> SB/vs-test-from-a, mirror + archive
job "A to B" "$(loc "$SA" vs-test)" "$(loc "$SB" vs-test-from-a)" mirror_archive
info "dry run: $(runjob 'A to B' dry-run)"
check "dry run did not create anything on $SB" "! exists $SB vs-test-from-a"
# watch the mounts while the real run copies
(for _ in $(seq 300); do grep ' cifs ' /proc/mounts; sleep 0.2; done) > "$WORK/mounts" &
W=$!
info "run 1:   $(runjob 'A to B')"
kill $W 2>/dev/null
check "source was mounted read-only (ro) by the kernel" \
  "grep '/$SA' $WORK/mounts | head -1 | grep -qE '\scifs\s+ro,'"
check "destination was mounted read-write" \
  "grep '/$SB' $WORK/mounts | head -1 | grep -qE '\scifs\s+rw,'"
check "all $NFILES files copied to $SB" \
  "[ \$(smb $SB 'ls vs-test-from-a/*' | grep -cE ' (data_[0-9]+\.bin|one\.txt|two\.txt) ') -eq $NFILES ]"
check "no mount left after the job" "! grep -q ' cifs ' /proc/mounts"
check "no credentials file left in /run" "[ -z \"\$(find /run -name '*cred*' -newer $AUTH 2>/dev/null)\" ]"
check "the copies kept the source modification time" \
  "mtime $SB vs-test-from-a/data_1.bin | grep -qE \"$OLD_SHOWN\""
R2=$(runjob 'A to B'); info "run 2:   $R2"
check "second run is incremental (0 files copied)" "echo '$R2' | grep -q ' 0 files copied'"

# deleted at the source -> moved to the archive in the destination
smb "$SA" 'cd vs-test; del two.txt' >/dev/null
info "run 3:   $(runjob 'A to B')"
check "two.txt removed from the mirror" "! exists $SB vs-test-from-a/two.txt"
check "two.txt kept in .vegasyncor-archive" \
  "smb $SB 'recurse; ls vs-test-from-a/.vegasyncor-archive/*' | grep -q two.txt"

# --- job 2: empty-source protection
smb "$SB" "mkdir vs-test-guard; cd vs-test-guard; lcd $WORK/fix; put one.txt" >/dev/null
job Guard "$(loc "$SA" vs-test-empty)" "$(loc "$SB" vs-test-guard)" mirror
RG=$(runjob Guard); info "guard:   $RG"
check "empty source blocks the mirror" "echo '$RG' | grep -q 'source is empty'"
check "backup in destination not wiped" "exists $SB vs-test-guard/one.txt"

# --- wrong password: clear error, no hang
BADID=$(api POST /api/connections "{\"name\":\"Bad\",\"host\":\"$H\",\"username\":\"$SMB_USER\",\"password\":\"wrong-$RANDOM\"}" | id)
check "connection test with wrong password explains the error" \
  "api POST /api/connections/$BADID/test | grep -q 'NT_STATUS_LOGON_FAILURE → wrong user or password'"
api POST /api/jobs "{\"name\":\"Bad pw\",\"enabled\":true,\"source\":{\"type\":\"smb\",\"connection_id\":\"$BADID\",\"share\":\"$SA\",\"path\":\"vs-test\"},\"source_readonly\":true,\"dest\":$(loc "$SB" vs-test-bad),\"mode\":\"mirror\",\"schedule\":{\"type\":\"manual\"}}" >/dev/null
RB=$(runjob 'Bad pw'); info "bad pw:  $RB"
check "wrong password fails with a hint" "echo '$RB' | grep -q 'Permission denied → '"
check "no mount left after failures" "! grep -q ' cifs ' /proc/mounts"

# --- service restart keeps configuration and history; no secrets on disk
systemctl restart vegasyncor
for _ in $(seq 50); do api GET /api/status >/dev/null && break; sleep 0.2; done
check "service restarts and keeps the 3 jobs" \
  "[ \$(vegasyncor status | grep -cE '^(A to B|Guard|Bad pw) ') -eq 3 ]"
check "passwords not in plain text in config, history, logs or journal" \
  "! grep -rqF -e \"\$SMB_PASS\" /etc/vegasyncor /var/lib/vegasyncor && ! journalctl -u vegasyncor | grep -qF -e \"\$SMB_PASS\""

# --- cleanup of the fixtures on SA
smb "$SA" 'deltree vs-test; deltree vs-test-empty' >/dev/null
check "$SA left as it was" "! exists $SA 'vs-test*'"

echo
echo "passed $pass, failed $failn"
[ "$failn" -eq 0 ]
