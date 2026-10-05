package daemon

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vegasyncor/internal/api"
)

func TestTrimHistory(t *testing.T) {
	var h []api.Run
	for i := range 10 {
		job := "a"
		if i%2 == 1 {
			job = "b"
		}
		h = append(h, api.Run{ID: fmt.Sprint(i), JobID: job})
	}
	keep, drop := trimHistory(h, 3)
	got := ""
	for _, r := range keep {
		got += r.ID
	}
	if got != "456789" {
		t.Errorf("keep = %s", got)
	}
	if len(drop) != 4 || drop[0].ID != "0" {
		t.Errorf("drop = %+v", drop)
	}
}

// A run left "in progress" (service killed abruptly) must show up as
// interrupted after the restart, with an end time.
func TestInterruptedRunAfterRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VEGASYNCOR_CONFIG_DIR", dir+"/etc")
	t.Setenv("VEGASYNCOR_STATE_DIR", dir+"/state")
	t.Setenv("VEGASYNCOR_RUNTIME_DIR", dir+"/run")
	t.Setenv("VEGASYNCOR_DEV", "1")
	t.Setenv("VEGASYNCOR_LANG", "en")

	d, err := New("test")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Minute)
	d.mu.Lock()
	d.addHistory(api.Run{ID: "r1", JobID: "j1", JobName: "Prova", Start: start, Status: api.StatusRunning})
	d.mu.Unlock()

	// "restart": a new service reads the history again
	d2, err := New("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.history) != 1 {
		t.Fatalf("history: %+v", d2.history)
	}
	h := d2.history[0]
	if h.Status != api.StatusError || h.End.IsZero() || !strings.Contains(h.Message, "interrupted") {
		t.Errorf("entry not marked as interrupted: %+v", h)
	}
	if d2.lastRun("j1") == nil {
		t.Error("the interrupted run must show up as the last run")
	}
}
