package mount

import (
	"strings"
	"testing"
)

func TestCleanOutput(t *testing.T) {
	out := "mount error(1): Operation not permitted\nRefer to the mount.cifs(8) manual page (e.g. man mount.cifs) and kernel log messages (dmesg)\n"
	got := cleanOutput(out)
	if got != "mount error(1): Operation not permitted" {
		t.Fatalf("cleanOutput = %q", got)
	}
	if e := explainMountError(got); !strings.Contains(e, "→") {
		t.Errorf("hint missing: %q", e)
	}
}

func TestParseCIFSMessages(t *testing.T) {
	out := `[  100.000000] CIFS: VFS: vecchio messaggio
[ 5000.120000] eth0: link up
[ 5000.500000] CIFS: Attempting to mount //192.168.1.20/Documenti
[ 5000.600000] CIFS: VFS: \\192.168.1.20 Send error in SessSetup = -13
[ 5000.610000] CIFS: VFS: cifs_mount failed w/return code = -13`
	got := parseCIFSMessages(out, 5000.4)
	if strings.Contains(got, "vecchio") || !strings.Contains(got, "SessSetup = -13") || strings.Count(got, ";") != 2 {
		t.Fatalf("parse = %q", got)
	}
}

func TestExplainMountErrorHints(t *testing.T) {
	cases := []struct{ msg, want string }{
		// real messages from smbclient and mount.cifs
		{"session setup failed: NT_STATUS_LOGON_FAILURE", "wrong user or password: check user"},
		{"tree connect failed: NT_STATUS_BAD_NETWORK_NAME", "share does not exist"},
		{"do_connect: Connection to 192.0.2.1 failed (Error NT_STATUS_IO_TIMEOUT)", "timeout"},
		{"mount.cifs: permission denied: no match for /run/vegasyncor/mnt/x-src found in /etc/fstab", "not running as root"},
		{"mount error(13): Permission denied", "wrong user or password, or insufficient"},
		{"mount error(112): Host is down", "server unreachable"},
	}
	for _, c := range cases {
		// repeated: with the old map the winning hint changed between calls
		for range 20 {
			if got := explainMountError(c.msg); !strings.Contains(got, "→ ") || !strings.Contains(got, c.want) {
				t.Fatalf("explainMountError(%q) = %q, want hint containing %q", c.msg, got, c.want)
			}
		}
	}
	if got := explainMountError("something unknown"); got != "something unknown" {
		t.Errorf("unknown message changed: %q", got)
	}
}
