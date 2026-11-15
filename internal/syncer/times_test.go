package syncer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vegasyncor/internal/config"
)

func TestFixTimes(t *testing.T) {
	dst := t.TempDir()
	sent := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := time.Now()
	write(t, filepath.Join(dst, "clobbered.txt"), "a")
	write(t, filepath.Join(dst, "sub dir", "with space.txt"), "b")
	write(t, filepath.Join(dst, "ok.txt"), "c")
	for _, f := range []string{"clobbered.txt", "sub dir/with space.txt"} {
		os.Chtimes(filepath.Join(dst, f), now, now) // the server stamped the copy time
	}
	okTime := sent.Add(time.Second) // within --modify-window: left alone
	os.Chtimes(filepath.Join(dst, "ok.txt"), okTime, okTime)

	log := filepath.Join(t.TempDir(), "times.log")
	write(t, log, strings.Join([]string{
		"2026/11/14 17:23:24 [15733] building file list",
		"2026/11/14 17:23:24 [15733] >f+++++++++ 2026/01/01-12:00:00 clobbered.txt",
		"2026/11/14 17:23:24 [15733] cd+++++++++ 2026/11/14-17:23:24 sub dir/",
		"2026/11/14 17:23:24 [15733] >f+++++++++ 2026/01/01-12:00:00 sub dir/with space.txt",
		"2026/11/14 17:23:24 [15733] >f+++++++++ 2026/01/01-12:00:00 ok.txt",
		"2026/11/14 17:23:24 [15733] >f+++++++++ 2026/01/01-12:00:00 vanished.txt",
		"2026/11/14 17:23:24 [15733] *deleting   old.txt",
		"2026/11/14 17:23:24 [15733] sent 231 bytes  received 71 bytes  total size 4",
	}, "\n")+"\n")

	n, err := fixTimes(dst, log)
	if err != nil || n != 2 {
		t.Fatalf("fixTimes = %d, %v; want 2 files corrected", n, err)
	}
	for f, want := range map[string]time.Time{"clobbered.txt": sent, "sub dir/with space.txt": sent, "ok.txt": okTime} {
		st, _ := os.Stat(filepath.Join(dst, f))
		if !st.ModTime().Equal(want) {
			t.Errorf("%s: mtime %v, want %v", f, st.ModTime(), want)
		}
	}
}

// TestRunWritesTimesLog checks that a real run passes the side log to rsync and
// leaves the destination with the source times (and no temporary files).
func TestRunWritesTimesLog(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a", "f.txt"), "hello")
	when := time.Date(2025, 6, 1, 8, 30, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(src, "a", "f.txt"), when, when)
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "vegasyncor-times-*"))

	var log bytes.Buffer
	o := Options{Src: src, Dst: dst, Mode: config.ModeMirror, RunStamp: "2026-01-01_000000"}
	if _, err := Run(context.Background(), o, &log, nil); err != nil {
		t.Fatalf("Run: %v\n%s", err, log.String())
	}
	st, err := os.Stat(filepath.Join(dst, "a", "f.txt"))
	if err != nil || !st.ModTime().Equal(when) {
		t.Fatalf("mtime not preserved: %v %v", st, err)
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "vegasyncor-times-*"))
	if len(after) > len(before) {
		t.Errorf("side log left in %s: %v", os.TempDir(), after)
	}
	if strings.Contains(log.String(), "--log-file") {
		t.Error("the side log should not appear in the run log")
	}
}
