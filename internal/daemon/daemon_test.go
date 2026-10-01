package daemon

import (
	"fmt"
	"testing"

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
