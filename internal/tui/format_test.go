package tui

import (
	"testing"
	"time"

	"vegasyncor/internal/i18n"
)

func TestFmtClockAndUntil(t *testing.T) {
	defer i18n.SetLang(i18n.Default)
	loc := time.FixedZone("CEST", 2*3600)
	at := time.Date(2026, 10, 5, 14, 3, 7, 0, loc)
	cases := []struct {
		lang, clock string
		until       map[time.Duration]string
	}{
		{"en", "Mon 2026-10-05 14:03:07 CEST", map[time.Duration]string{
			30 * time.Second:              "in less than a minute",
			12 * time.Minute:              "in 12 min",
			7*time.Hour + 5*time.Minute:   "in 7h 05m",
			50*time.Hour + 10*time.Minute: "in 2 d 2h",
		}},
		{"it", "Lun 05/10/2026 14:03:07 CEST", map[time.Duration]string{
			30 * time.Second:              "tra meno di un minuto",
			12 * time.Minute:              "tra 12 min",
			7*time.Hour + 5*time.Minute:   "tra 7h 05m",
			50*time.Hour + 10*time.Minute: "tra 2 g 2h",
		}},
	}
	for _, c := range cases {
		i18n.SetLang(c.lang)
		if got := fmtClock(at, "CEST"); got != c.clock {
			t.Errorf("%s: fmtClock = %q, want %q", c.lang, got, c.clock)
		}
		for d, want := range c.until {
			if got := fmtUntil(d); got != want {
				t.Errorf("%s: fmtUntil(%v) = %q, want %q", c.lang, d, got, want)
			}
		}
	}
}
