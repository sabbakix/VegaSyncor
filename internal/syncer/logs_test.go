package syncer

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"Accounting":       "Accounting",
		`Docs: A/B*?`:      "Docs_ A_B__",
		"Report. ":         "Report",
		"":                 "job",
		"Contabilità 2026": "Contabilità 2026",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveAndCompressLogs(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "run.log")
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	save := func(job string, at time.Time, dry bool) string {
		os.WriteFile(src, []byte(job+" "+at.String()), 0o644)
		p, err := SaveLog(dir, job, at.Format(StampFormat), dry, src)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Base(p)
	}
	save("Docs", time.Date(2026, 8, 30, 22, 0, 0, 0, time.Local), false)
	save("Docs", time.Date(2026, 9, 1, 22, 0, 0, 0, time.Local), true)
	save("Docs", time.Date(2026, 9, 20, 22, 0, 0, 0, time.Local), false)
	recent := save("Docs", time.Date(2026, 10, 6, 22, 0, 0, 0, time.Local), false)
	other := save("Docs archive", time.Date(2026, 8, 1, 22, 0, 0, 0, time.Local), false) // another job
	if recent != "Docs_2026-10-06_220000.log" {
		t.Errorf("log name = %q", recent)
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)

	n, err := CompressLogs(dir, "Docs", 30, now)
	if err != nil || n != 2 { // Sep 20 is still within 30 days
		t.Fatalf("compressed %d, %v", n, err)
	}
	// a later compression adds to the existing monthly zip
	save("Docs", time.Date(2026, 9, 2, 8, 0, 0, 0, time.Local), false)
	if n, err := CompressLogs(dir, "Docs", 30, now); err != nil || n != 1 {
		t.Fatalf("second compression: %d, %v", n, err)
	}

	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"Docs archive_2026-08-01_220000.log", "Docs_2026-09-20_220000.log", "Docs_2026-10-06_220000.log",
		"Docs_logs_2026-08.zip", "Docs_logs_2026-09.zip", "notes.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("files = %v", names)
	}
	_ = other
	r, err := zip.OpenReader(filepath.Join(dir, "Docs_logs_2026-09.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var inZip []string
	for _, f := range r.File {
		inZip = append(inZip, f.Name)
	}
	sort.Strings(inZip)
	if strings.Join(inZip, ",") != "Docs_2026-09-01_220000_dry-run.log,Docs_2026-09-02_080000.log" {
		t.Errorf("zip content = %v", inZip)
	}
}
