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
	removed, err := PruneArchive(dst, 30, now)
	if err != nil || len(removed) != 1 || removed[0] != "2026-08-01_220000" {
		t.Fatalf("%v %v", removed, err)
	}
}

// The archive folder of older versions is renamed and never deleted by a mirror.
func TestLegacyArchive(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "a")
	legacy := filepath.Join(dst, config.LegacyArchiveDirName, "2026-09-01_220000", "old.txt")
	write(t, legacy, "archived")

	// a dry run must not list the old folder among the files to delete
	var log bytes.Buffer
	res, err := Run(context.Background(), Options{Src: src, Dst: dst, Mode: config.ModeMirror, DryRun: true, RunStamp: "x"}, &log, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.FilesDeleted != 0 || strings.Contains(log.String(), "*deleting") {
		t.Fatalf("legacy archive treated as files to delete:\n%s", log.String())
	}

	moved, err := MigrateArchive(dst)
	if err != nil || !moved {
		t.Fatalf("MigrateArchive = %v, %v", moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, config.ArchiveDirName, "2026-09-01_220000", "old.txt")); string(b) != "archived" {
		t.Error("archived file not found in the new folder")
	}
	if moved, _ := MigrateArchive(dst); moved {
		t.Error("second migration should do nothing")
	}

	// both folders present: the old one stays and is still excluded
	write(t, legacy, "archived")
	if moved, _ := MigrateArchive(dst); moved {
		t.Error("must not overwrite an existing new folder")
	}
	if _, err := Run(context.Background(), Options{Src: src, Dst: dst, Mode: config.ModeMirror, RunStamp: "y"}, &log, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Error("mirror deleted the legacy archive folder")
	}
}
