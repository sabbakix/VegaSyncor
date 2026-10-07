package tui

import (
	"strings"
	"testing"
)

const sampleLog = `[22:00:01] Job "Docs" (Mirror) – real run
# rsync -rt ...
.d..t...... ./
.d..t...... 2026/
>f+++++++++ 2026/10/Invoice-1.pdf
>f.st...... Ledger.xlsx
*deleting   Old/Invoice-1.pdf
cd+++++++++ Archive/Site A/
>f+++++++++ Archive/Site A/plan.dwg
>f+++++++++ Archive/Site A/sub/notes.txt
*deleting   Projects/Site A/sub/notes.txt
*deleting   Projects/Site A/plan.dwg
*deleting   Projects/Site A/sub/
*deleting   Projects/Site A/
>f+++++++++ a/readme.txt
>f+++++++++ b/readme.txt
*deleting   c/readme.txt
*deleting   Temp.tmp
cd+++++++++ Archive/2026-02/
>f+++++++++ Archive/2026-02/Invoice-7.pdf
*deleting   2026/02/Invoice-7.pdf
*deleting   2026/02/
rsync: [sender] send_files failed to open "Locked.xlsx": Permission denied (13)
Number of files: 12`

func TestParseLogMoves(t *testing.T) {
	es := parseLog(sampleLog)
	moved := map[string]string{}
	var newF, del, upd []string
	for _, e := range es {
		switch e.kind {
		case ekMoved:
			moved[e.path] = e.from
		case ekNew:
			newF = append(newF, e.path)
		case ekDeleted:
			del = append(del, e.path)
		case ekUpdated:
			upd = append(upd, e.path)
		}
	}
	want := map[string]string{
		"2026/10/Invoice-1.pdf":         "Old/Invoice-1.pdf",
		"Archive/Site A/plan.dwg":       "Projects/Site A/plan.dwg",
		"Archive/Site A/sub/notes.txt":  "Projects/Site A/sub/notes.txt",
		"Archive/Site A/":               "Projects/Site A/",
		"Archive/2026-02/Invoice-7.pdf": "2026/02/Invoice-7.pdf",
		"Archive/2026-02/":              "2026/02/", // renamed folder
	}
	for k, v := range want {
		if moved[k] != v {
			t.Errorf("moved %q: from %q, want %q", k, moved[k], v)
		}
	}
	if len(moved) != len(want) {
		t.Errorf("moved = %v", moved)
	}
	// a/readme.txt and b/readme.txt match c/readme.txt equally well: no guess
	if strings.Join(newF, ",") != "a/readme.txt,b/readme.txt" {
		t.Errorf("new = %v", newF)
	}
	if strings.Join(del, ",") != "Projects/Site A/sub/,c/readme.txt,Temp.tmp" {
		t.Errorf("deleted = %v", del)
	}
	if strings.Join(upd, ",") != "Ledger.xlsx" {
		t.Errorf("updated = %v", upd)
	}
	c := logCounts(es)
	if c[lfMoved] != 6 || c[lfNew] != 2 || c[lfDeleted] != 3 || c[lfUpdated] != 1 || c[lfChanges] != 12 {
		t.Errorf("counts = %v", c)
	}
}

func TestRenderLogFilters(t *testing.T) {
	es := parseLog(sampleLog)
	out := renderLog(es, lfChanges)
	if strings.Contains(out, "unchanged") || strings.Contains(out, "2026/\n") || strings.Contains(out, "Number of files") {
		t.Errorf("changes view shows unchanged rows or statistics:\n%s", out)
	}
	if !strings.Contains(out, "Locked.xlsx") {
		t.Error("errors must be shown with every filter")
	}
	moved := renderLog(es, lfMoved)
	if !strings.Contains(moved, "Old/Invoice-1.pdf") || !strings.Contains(moved, "-> 2026/10/Invoice-1.pdf") || strings.Contains(moved, "Temp.tmp") {
		t.Errorf("moved view:\n%s", moved)
	}
	if !strings.Contains(renderLog(parseLog("[x] nothing"), lfMoved), "No rows") {
		t.Error("empty filter without message")
	}
}
