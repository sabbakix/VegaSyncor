package tui

import (
	"fmt"
	"testing"
	"time"

	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

// The [browse] button must stay on screen to be visible and clickable.
func TestBrowseButtonVisible(t *testing.T) {
	zone.NewGlobal()
	for _, w := range []int{80, 118, 160} {
		m := &Model{w: w, h: 50, st: &api.Status{Connections: []api.ConnectionView{{Connection: config.Connection{ID: "c1", Name: "PC", Host: "PC"}}}}}
		j := config.Job{Name: "x", Source: config.Location{Type: "smb", ConnectionID: "c1", Share: "Documenti"},
			Dest: config.Location{Type: "local", Path: "/tmp"}, Mode: "mirror", Schedule: config.Schedule{Type: "manual"}}
		m.form = newJobForm(&j, m.st.Connections, "")
		m.View()
		time.Sleep(50 * time.Millisecond) // zone.Scan records the zones asynchronously
		for i, f := range m.form.Fields {
			if !f.Browse || !m.form.visible(f) {
				continue
			}
			z := zone.Get(fmt.Sprintf("browse:%d", i))
			if z == nil || z.EndX >= w {
				t.Errorf("width %d, field %s: button off screen: %+v", w, f.Key, z)
			}
		}
	}
}
