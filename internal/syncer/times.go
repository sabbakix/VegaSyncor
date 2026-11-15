package syncer

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Some SMB servers (seen with Samba on OpenMediaVault) overwrite the
// modification time that rsync sets on a new file with the time of the copy,
// because the write completes on the server after rsync has set the time.
// The next run then sees every file as changed: it copies everything again
// and, in Mirror + archive, fills the archive with identical copies.
//
// To fix it, rsync also writes a side log (--log-file) with the modification
// time it sent for each received file (%M), and fixTimes resets the files whose
// time on the destination differs. The time comes from the very version that
// was copied, so a file changed at the source in the meantime can't get a
// newer time on older content.

// timesLogFormat is the --log-file-format: itemize, sent mtime, name.
const timesLogFormat = "%i %M %n"

// timesLogRe matches a received regular file in the side log, e.g.
// "2026/11/14 17:23:24 [15733] >f+++++++++ 2026/01/01-12:00:00 sub/b.txt".
var timesLogRe = regexp.MustCompile(`^\S+ \S+ \[\d+\] >f\S{9} (\d{4}/\d\d/\d\d-\d\d:\d\d:\d\d) (.+)$`)

// timesLayout is the layout of %M; Run sets TZ=UTC so it is in UTC.
const timesLayout = "2006/01/02-15:04:05"

// timesTolerance matches --modify-window=2: smaller differences are ignored.
const timesTolerance = 2 * time.Second

// fixTimes reads the side log written by rsync and resets the modification time
// of the received files whose time in dst differs from the one rsync sent.
// It returns how many files it corrected; files it can't read are skipped.
func fixTimes(dst, logPath string) (int, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fixed := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := timesLogRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		want, err := time.ParseInLocation(timesLayout, m[1], time.UTC)
		if err != nil {
			continue
		}
		p := filepath.Join(dst, m[2])
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue // escaped name (\#ooo), removed meanwhile, ...
		}
		if d := st.ModTime().Sub(want); d > -timesTolerance && d < timesTolerance {
			continue
		}
		if err := os.Chtimes(p, time.Time{}, want); err != nil {
			return fixed, err
		}
		fixed++
	}
	return fixed, sc.Err()
}
