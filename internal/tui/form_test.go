package tui

import (
	"testing"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

// Fields remember their initial value: only the changed ones are reported as modified.
func TestFormModified(t *testing.T) {
	conns := []api.ConnectionView{{Connection: config.Connection{ID: "c1", Name: "PC", Host: "PC"}}}
	j := config.Job{Name: "Job", Source: config.Location{Type: "smb", ConnectionID: "c1", Share: "Docs"},
		Dest: config.Location{Type: "local", Path: "/srv"}, Mode: config.ModeMirror,
		Schedule: config.Schedule{Type: config.SchedManual}}
	fm := newJobForm(&j, conns, "")
	for _, f := range fm.Fields {
		if f.modified() {
			t.Fatalf("field %s modified right after opening", f.Key)
		}
	}
	fm.get("dst_path").setValue("/srv/new")
	fm.get("src_ro").Bool = !fm.get("src_ro").Bool
	mode := fm.get("mode")
	mode.Sel = (mode.Sel + 1) % len(mode.Options)
	for _, f := range fm.Fields {
		want := f.Key == "dst_path" || f.Key == "src_ro" || f.Key == "mode"
		if f.modified() != want {
			t.Errorf("field %s: modified = %v, want %v", f.Key, f.modified(), want)
		}
	}
	// back to the original value: no longer modified
	fm.get("dst_path").setValue("/srv")
	if fm.get("dst_path").modified() {
		t.Error("restored value still reported as modified")
	}
}
