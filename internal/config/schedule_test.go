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
	// 2026-10-01 is a Thursday
	cases := []struct {
		name string
		s    Schedule
		now  string
		want string
	}{
		{"interval 30m", Schedule{Type: SchedInterval, EveryMinutes: 30}, "2026-10-01 10:07", "2026-10-01 10:30"},
		{"interval exact", Schedule{Type: SchedInterval, EveryMinutes: 30}, "2026-10-01 10:30", "2026-10-01 11:00"},
		{"interval with window", Schedule{Type: SchedInterval, EveryMinutes: 60, WindowFrom: "08:00", WindowTo: "18:00"}, "2026-10-01 18:30", "2026-10-02 08:00"},
		{"interval Mon-Fri", Schedule{Type: SchedInterval, EveryMinutes: 120, Days: []int{1, 2, 3, 4, 5}}, "2026-10-02 23:00", "2026-10-05 00:00"},
		{"daily several times", Schedule{Type: SchedDaily, Times: []string{"13:00", "22:30"}}, "2026-10-01 14:00", "2026-10-01 22:30"},
		{"weekly", Schedule{Type: SchedWeekly, Times: []string{"22:00"}, Days: []int{1, 3}}, "2026-10-01 23:00", "2026-10-05 22:00"},
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
		t.Error("manual must return zero")
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
		t.Errorf("normalisation: %+v %+v", j.Source, j.Dest)
	}
	j.Source.Path = "../etc"
	if j.Validate(c) == nil {
		t.Error("path with .. accepted")
	}
	j2 := Job{Name: "y", Source: Location{Type: LocLocal, Path: "/data"}, Dest: Location{Type: LocLocal, Path: "/data/bk"},
		Mode: ModeMirror, Schedule: Schedule{Type: SchedManual}}
	if j2.Validate(c) == nil {
		t.Error("nested folders accepted")
	}
}

func TestDestConflict(t *testing.T) {
	c := &Config{Connections: []Connection{
		{ID: "c1", Host: "NAS", Username: "a"},
		{ID: "c2", Host: "nas", Username: "b"}, // same server, other user
		{ID: "c3", Host: "OTHER", Username: "a"},
	}}
	smb := func(conn, share, path string) Location {
		return Location{Type: LocSMB, ConnectionID: conn, Share: share, Path: path}
	}
	local := func(p string) Location { return Location{Type: LocLocal, Path: p} }
	cases := []struct {
		name         string
		a, b         Location
		modeA, modeB string
		conflict     bool
	}{
		{"same folder, different connection to same host", smb("c1", "Backup", "Acc"), smb("c2", "backup", "acc"), ModeMirror, ModeAdditive, true},
		{"nested (b inside a)", smb("c1", "Backup", ""), smb("c1", "Backup", "Acc"), ModeAdditive, ModeMirrorArchive, true},
		{"nested (a inside b)", smb("c1", "Backup", "Acc/2026"), smb("c1", "Backup", "Acc"), ModeMirror, ModeMirror, true},
		{"sibling with common prefix", smb("c1", "Backup", "A"), smb("c1", "Backup", "AB"), ModeMirror, ModeMirror, false},
		{"different server", smb("c1", "Backup", "A"), smb("c3", "Backup", "A"), ModeMirror, ModeMirror, false},
		{"different share", smb("c1", "Backup", "A"), smb("c1", "Archive", "A"), ModeMirror, ModeMirror, false},
		{"two add-only jobs", smb("c1", "Backup", "A"), smb("c1", "Backup", "A"), ModeAdditive, ModeAdditive, false},
		{"local nested", local("/srv/backup"), local("/srv/backup/acc"), ModeMirror, ModeAdditive, true},
		{"local siblings", local("/srv/backup/a"), local("/srv/backup/ab"), ModeMirror, ModeMirror, false},
		{"local vs smb", local("/srv/backup"), smb("c1", "Backup", ""), ModeMirror, ModeMirror, false},
	}
	for _, tc := range cases {
		cfg := *c
		cfg.Jobs = []Job{{ID: "b", Name: "B", Dest: tc.b, Mode: tc.modeB}}
		j := Job{ID: "a", Name: "A", Dest: tc.a, Mode: tc.modeA}
		if got := cfg.DestConflict(j) != nil; got != tc.conflict {
			t.Errorf("%s: conflict = %v, want %v", tc.name, got, tc.conflict)
		}
		cfg.Jobs = append(cfg.Jobs, j)
		if got := len(cfg.DestConflicts()) == 1; got != tc.conflict {
			t.Errorf("%s: DestConflicts = %v", tc.name, cfg.DestConflicts())
		}
	}
	// a job is never in conflict with itself (editing an existing job)
	self := &Config{Jobs: []Job{{ID: "a", Dest: local("/srv/b"), Mode: ModeMirror}}}
	if self.DestConflict(self.Jobs[0]) != nil {
		t.Error("job in conflict with itself")
	}
}

func TestCleanFolderName(t *testing.T) {
	ok := map[string]string{"Backup 2026": "Backup 2026", "  Accounting ": "Accounting", "è-ok_(1)": "è-ok_(1)"}
	for in, want := range ok {
		if got, err := CleanFolderName(in); err != nil || got != want {
			t.Errorf("CleanFolderName(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "  ", ".", "..", "a/b", `a\b`, "a:b", "what?", "name.", "tab\there"} {
		if _, err := CleanFolderName(bad); err == nil {
			t.Errorf("CleanFolderName(%q) accepted", bad)
		}
	}
}
