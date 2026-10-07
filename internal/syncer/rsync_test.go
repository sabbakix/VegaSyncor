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

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModes(t *testing.T) {
	for _, mode := range config.Modes {
		t.Run(mode, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			write(t, filepath.Join(src, "a.txt"), "uno")
			write(t, filepath.Join(src, "sub", "b.txt"), "due")
			write(t, filepath.Join(src, "Thumbs.db"), "x")
			write(t, filepath.Join(dst, "vecchio.txt"), "da cancellare")
			write(t, filepath.Join(dst, "a.txt"), "versione precedente diversa")
			past := time.Now().Add(-time.Hour)
			os.Chtimes(filepath.Join(dst, "a.txt"), past, past)

			var log bytes.Buffer
			stamp := "2026-10-01_220000"
			// first a dry run: it must not change anything
			res, err := Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, DryRun: true, RunStamp: stamp}, &log, nil)
			if err != nil {
				t.Fatalf("dry: %v\n%s", err, log.String())
			}
			if _, err := os.Stat(filepath.Join(dst, "sub")); err == nil {
				t.Fatal("the dry run wrote files")
			}
			if res.Stats.FilesTransferred != 2 {
				t.Errorf("dry: files to transfer = %d", res.Stats.FilesTransferred)
			}

			log.Reset()
			var lastProg Progress
			res, err = Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, RunStamp: stamp}, &log, func(p Progress) { lastProg = p })
			if err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "due" {
				t.Error("b.txt not copied")
			}
			if _, err := os.Stat(filepath.Join(dst, "Thumbs.db")); err == nil {
				t.Error("Thumbs.db not excluded")
			}
			_, oldErr := os.Stat(filepath.Join(dst, "vecchio.txt"))
			arch := filepath.Join(dst, config.ArchiveDirName, stamp)
			switch mode {
			case config.ModeAdditive:
				if oldErr != nil {
					t.Error("additive deleted files")
				}
			case config.ModeMirror:
				if oldErr == nil {
					t.Error("mirror did not delete")
				}
			case config.ModeMirrorArchive:
				if oldErr == nil {
					t.Error("mirror did not delete")
				}
				if b, _ := os.ReadFile(filepath.Join(arch, "vecchio.txt")); string(b) != "da cancellare" {
					t.Error("deleted file not archived")
				}
				if b, _ := os.ReadFile(filepath.Join(arch, "a.txt")); string(b) != "versione precedente diversa" {
					t.Error("previous version not archived")
				}
			}
			if res.Stats.FilesTransferred != 2 {
				t.Errorf("transferred = %d\n%s", res.Stats.FilesTransferred, log.String())
			}
			_ = lastProg
			if !strings.Contains(log.String(), "b.txt") {
				t.Errorf("log without the list of files:\n%s", log.String())
			}

			// second run: the archive must not be touched or copied again
			log.Reset()
			res, err = Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, RunStamp: "2026-10-02_220000"}, &log, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Stats.FilesTransferred != 0 || res.Stats.FilesDeleted != 0 {
				t.Errorf("second run not idempotent: %+v\n%s", res.Stats, log.String())
			}
			if mode == config.ModeMirrorArchive {
				if _, err := os.Stat(arch); err != nil {
					t.Error("archive removed by the mirror")
				}
			}
		})
	}
}

func TestPrune(t *testing.T) {
	dst := t.TempDir()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	for _, d := range []string{"2026-08-01_220000", "2026-09-25_220000", "altro"} {
		os.MkdirAll(filepath.Join(dst, config.ArchiveDirName, d), 0o755)
	}
	removed, err := PruneArchive(filepath.Join(dst, config.ArchiveDirName), 30, now)
	if err != nil || len(removed) != 1 || removed[0] != "2026-08-01_220000" {
		t.Fatalf("%v %v", removed, err)
	}
}

// With the same size and modification time a change is seen only with Checksum.
func TestChecksum(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "f.txt"), "new!")
	write(t, filepath.Join(dst, "f.txt"), "old!")
	when := time.Now().Add(-time.Hour).Truncate(time.Second)
	os.Chtimes(filepath.Join(src, "f.txt"), when, when)
	os.Chtimes(filepath.Join(dst, "f.txt"), when, when)
	var log bytes.Buffer
	o := Options{Src: src, Dst: dst, Mode: config.ModeMirror, RunStamp: "2026-10-01_220000"}
	if _, err := Run(context.Background(), o, &log, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(b) != "old!" {
		t.Fatal("quick check copied a file with the same size and time")
	}
	o.Checksum = true
	if _, err := Run(context.Background(), o, &log, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(b) != "new!" {
		t.Error("checksum did not detect the changed contents")
	}
}

// Custom archive folder outside the destination, and protected folders inside
// it (a log or archive folder chosen in the destination) left alone by the mirror.
func TestCustomArchiveAndProtect(t *testing.T) {
	src, dst, arc := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "new")
	write(t, filepath.Join(dst, "a.txt"), "old version")
	write(t, filepath.Join(dst, "gone.txt"), "deleted at the source")
	write(t, filepath.Join(dst, "_logs [x]", "Docs_2026-10-01_220000.log"), "log")
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dst, "a.txt"), past, past)
	stamp := "2026-10-07_220000"
	var log bytes.Buffer
	_, err := Run(context.Background(), Options{Src: src, Dst: dst, Mode: config.ModeMirrorArchive, RunStamp: stamp,
		ArchiveDir: arc, Protect: []string{"_logs [x]"}}, &log, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(arc, stamp, "gone.txt")); string(b) != "deleted at the source" {
		t.Error("deleted file not moved to the custom archive")
	}
	if b, _ := os.ReadFile(filepath.Join(arc, stamp, "a.txt")); string(b) != "old version" {
		t.Error("previous version not moved to the custom archive")
	}
	if _, err := os.Stat(filepath.Join(dst, config.ArchiveDirName)); err == nil {
		t.Error("default archive folder created anyway")
	}
	if _, err := os.Stat(filepath.Join(dst, "_logs [x]", "Docs_2026-10-01_220000.log")); err != nil {
		t.Error("protected folder deleted by the mirror")
	}
}
