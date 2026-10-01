package config

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func TestScheduleNext(t *testing.T) {
	// 2026-10-01 è un giovedì
	cases := []struct {
		name string
		s    Schedule
		now  string
		want string
	}{
		{"intervallo 30m", Schedule{Type: SchedInterval, EveryMinutes: 30}, "2026-10-01 10:07", "2026-10-01 10:30"},
		{"intervallo esatto", Schedule{Type: SchedInterval, EveryMinutes: 30}, "2026-10-01 10:30", "2026-10-01 11:00"},
		{"intervallo con fascia", Schedule{Type: SchedInterval, EveryMinutes: 60, WindowFrom: "08:00", WindowTo: "18:00"}, "2026-10-01 18:30", "2026-10-02 08:00"},
		{"intervallo lun-ven", Schedule{Type: SchedInterval, EveryMinutes: 120, Days: []int{1, 2, 3, 4, 5}}, "2026-10-02 23:00", "2026-10-05 00:00"},
		{"giornaliero più orari", Schedule{Type: SchedDaily, Times: []string{"13:00", "22:30"}}, "2026-10-01 14:00", "2026-10-01 22:30"},
		{"settimanale", Schedule{Type: SchedWeekly, Times: []string{"22:00"}, Days: []int{1, 3}}, "2026-10-01 23:00", "2026-10-05 22:00"},
		{"cron", Schedule{Type: SchedCron, Cron: "15 */2 * * *"}, "2026-10-01 10:20", "2026-10-01 12:15"},
	}
	for _, c := range cases {
		if err := c.s.Validate(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := c.s.Next(at(c.now))
		if !got.Equal(at(c.want)) {
			t.Errorf("%s: got %s want %s", c.name, got.Format("2006-01-02 15:04 Mon"), c.want)
		}
	}
	if !(&Schedule{Type: SchedManual}).Next(time.Now()).IsZero() {
		t.Error("manuale deve restituire zero")
	}
}

func TestValidate(t *testing.T) {
	c := &Config{Connections: []Connection{{ID: "c1", Name: "pc", Host: "10.0.0.1", Username: "u"}}}
	j := Job{Name: "x", Source: Location{Type: LocSMB, ConnectionID: "c1", Share: "Doc", Path: `a\b\`},
		Dest: Location{Type: LocLocal, Path: "/srv/bk/"}, Mode: ModeMirror, Schedule: Schedule{Type: SchedManual}}
	if err := j.Validate(c); err != nil {
		t.Fatal(err)
	}
	if j.Source.Path != "a/b" || j.Dest.Path != "/srv/bk" {
		t.Errorf("normalizzazione: %+v %+v", j.Source, j.Dest)
	}
	j.Source.Path = "../etc"
	if j.Validate(c) == nil {
		t.Error("percorso con .. accettato")
	}
	j2 := Job{Name: "y", Source: Location{Type: LocLocal, Path: "/data"}, Dest: Location{Type: LocLocal, Path: "/data/bk"},
		Mode: ModeMirror, Schedule: Schedule{Type: SchedManual}}
	if j2.Validate(c) == nil {
		t.Error("cartelle annidate accettate")
	}
}
