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
			// prima una simulazione: non deve cambiare nulla
			res, err := Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, DryRun: true, RunStamp: stamp}, &log, nil)
			if err != nil {
				t.Fatalf("dry: %v\n%s", err, log.String())
			}
			if _, err := os.Stat(filepath.Join(dst, "sub")); err == nil {
				t.Fatal("la simulazione ha scritto file")
			}
			if res.Stats.FilesTransferred != 2 {
				t.Errorf("dry: file da trasferire = %d", res.Stats.FilesTransferred)
			}

			log.Reset()
			var lastProg Progress
			res, err = Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, RunStamp: stamp}, &log, func(p Progress) { lastProg = p })
			if err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "due" {
				t.Error("b.txt non copiato")
			}
			if _, err := os.Stat(filepath.Join(dst, "Thumbs.db")); err == nil {
				t.Error("Thumbs.db non escluso")
			}
			_, oldErr := os.Stat(filepath.Join(dst, "vecchio.txt"))
			arch := filepath.Join(dst, config.ArchiveDirName, stamp)
			switch mode {
			case config.ModeAdditive:
				if oldErr != nil {
					t.Error("additive ha cancellato")
				}
			case config.ModeMirror:
				if oldErr == nil {
					t.Error("mirror non ha cancellato")
				}
			case config.ModeMirrorArchive:
				if oldErr == nil {
					t.Error("mirror non ha cancellato")
				}
				if b, _ := os.ReadFile(filepath.Join(arch, "vecchio.txt")); string(b) != "da cancellare" {
					t.Error("file cancellato non archiviato")
				}
				if b, _ := os.ReadFile(filepath.Join(arch, "a.txt")); string(b) != "versione precedente diversa" {
					t.Error("versione precedente non archiviata")
				}
			}
			if res.Stats.FilesTransferred != 2 {
				t.Errorf("trasferiti = %d\n%s", res.Stats.FilesTransferred, log.String())
			}
			_ = lastProg
			if !strings.Contains(log.String(), "b.txt") {
				t.Errorf("log senza elenco file:\n%s", log.String())
			}

			// seconda esecuzione: l'archivio non deve essere toccato né ricopiato
			log.Reset()
			res, err = Run(context.Background(), Options{Src: src, Dst: dst, Mode: mode, RunStamp: "2026-10-02_220000"}, &log, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Stats.FilesTransferred != 0 || res.Stats.FilesDeleted != 0 {
				t.Errorf("seconda esecuzione non idempotente: %+v\n%s", res.Stats, log.String())
			}
			if mode == config.ModeMirrorArchive {
				if _, err := os.Stat(arch); err != nil {
					t.Error("archivio rimosso dal mirror")
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
