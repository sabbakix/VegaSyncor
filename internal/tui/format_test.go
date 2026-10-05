package tui

import (
	"testing"
	"time"
)

func TestFmtClockAndUntil(t *testing.T) {
	loc := time.FixedZone("CEST", 2*3600)
	if got := fmtClock(time.Date(2026, 10, 5, 14, 3, 7, 0, loc), "CEST"); got != "lun 05/10/2026 14:03:07 CEST" {
		t.Errorf("fmtClock = %q", got)
	}
	cases := map[time.Duration]string{
		30 * time.Second:              "tra meno di un minuto",
		12 * time.Minute:              "tra 12 min",
		7*time.Hour + 5*time.Minute:   "tra 7h 05m",
		50*time.Hour + 10*time.Minute: "tra 2 g 2h",
	}
	for d, want := range cases {
		if got := fmtUntil(d); got != want {
			t.Errorf("fmtUntil(%v) = %q, want %q", d, got, want)
		}
	}
}
