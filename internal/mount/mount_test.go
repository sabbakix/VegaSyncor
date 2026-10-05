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
