package config

import (
	"strings"
	"testing"
)

func foldersConfig() (*Config, func(conn, share, path string) *Location, func(p string) *Location) {
	c := &Config{Connections: []Connection{
		{ID: "pc", Host: "PC", Username: "a"},
		{ID: "nas", Host: "NAS", Username: "a"},
		{ID: "nas2", Host: "nas", Username: "b"},
	}}
	smb := func(conn, share, path string) *Location {
		return &Location{Type: LocSMB, ConnectionID: conn, Share: share, Path: path}
	}
	local := func(p string) *Location { return &Location{Type: LocLocal, Path: p} }
	return c, smb, local
}

func TestRelPath(t *testing.T) {
	c, smb, local := foldersConfig()
	cases := []struct {
		child, parent *Location
		rel           string
		ok            bool
	}{
		{smb("nas", "Backup", "Acc/_deleted"), smb("nas", "Backup", "Acc"), "_deleted", true},
		{smb("nas2", "backup", "ACC/Logs/2026"), smb("nas", "Backup", "Acc"), "Logs/2026", true}, // other user, case
		{smb("nas", "Backup", "Acc"), smb("nas", "Backup", "Acc"), "", true},
		{smb("nas", "Backup", "Accounting"), smb("nas", "Backup", "Acc"), "", false},
		{smb("nas", "Backup", "x"), smb("nas", "Backup", ""), "x", true},
		{smb("nas", "Other", "Acc/x"), smb("nas", "Backup", "Acc"), "", false},
		{local("/srv/backup/acc/.del"), local("/srv/backup/acc"), ".del", true},
		{local("/srv/backup/accx"), local("/srv/backup/acc"), "", false},
		{local("/srv/backup"), local("/srv/backup/acc"), "", false},
	}
	for _, tc := range cases {
		rel, ok := c.RelPath(*tc.child, *tc.parent)
		if rel != tc.rel || ok != tc.ok {
			t.Errorf("RelPath(%+v, %+v) = %q %v, want %q %v", *tc.child, *tc.parent, rel, ok, tc.rel, tc.ok)
		}
	}
}

func TestValidateFolders(t *testing.T) {
	c, smb, local := foldersConfig()
	job := func(arc, logd *Location) Job {
		return Job{Name: "Acc", Source: *smb("pc", "Docs", "Acc"), SourceRO: true, Dest: *smb("nas", "Backup", "Acc"),
			Mode: ModeMirrorArchive, Archive: arc, LogDir: logd, Schedule: Schedule{Type: SchedManual}}
	}
	cases := []struct {
		name      string
		arc, logd *Location
		err       string
	}{
		{"defaults", nil, nil, ""},
		{"archive on another share", smb("nas", "Deleted", "Acc"), nil, ""},
		{"archive inside the destination", smb("nas", "Backup", `Acc\_deleted`), nil, ""},
		{"logs on a local disk", nil, local("/var/log/vegasyncor-jobs"), ""},
		{"archive in the source", smb("pc", "Docs", "Acc/old"), nil, "source"},
		{"log folder containing the source", nil, smb("pc", "Docs", ""), "source"},
		{"archive = destination", smb("nas", "Backup", "Acc"), nil, "destination"},
		{"log folder containing the destination", nil, smb("nas2", "backup", ""), "destination"},
		{"archive without share", smb("nas", "", "x"), nil, "share"},
		{"relative local path", nil, local("logs"), "absolute"},
	}
	for _, tc := range cases {
		j := job(tc.arc, tc.logd)
		err := j.Validate(c)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: unexpected error %v", tc.name, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: error = %v, want something about %q", tc.name, err, tc.err)
		}
	}
	// the archive folder is dropped when the mode does not archive
	j := job(smb("nas", "Deleted", ""), nil)
	j.Mode = ModeMirror
	if err := j.Validate(c); err != nil || j.Archive != nil {
		t.Errorf("mirror job kept the archive folder: %v %+v", err, j.Archive)
	}
	if got := job(smb("nas", "Deleted", ""), local("/logs")).ConnectionIDs(); strings.Join(got, ",") != "pc,nas" {
		t.Errorf("ConnectionIDs = %v", got)
	}
}

func TestFolderConflict(t *testing.T) {
	c, smb, _ := foldersConfig()
	c.Jobs = []Job{
		{ID: "o", Name: "Other", Dest: *smb("nas", "Backup", "Other"), Mode: ModeMirror},
		{ID: "k", Name: "Keeper", Dest: *smb("nas", "Backup", "Keeper"), Mode: ModeMirrorArchive, Archive: smb("nas", "Deleted", "Keeper"), LogDir: smb("nas", "Logs", "")},
	}
	j := Job{ID: "a", Name: "A", Dest: *smb("nas", "Backup", "A"), Mode: ModeMirrorArchive}
	cases := []struct {
		name string
		edit func(j *Job)
		want string
	}{
		{"no conflict", func(j *Job) { j.Archive = smb("nas", "Deleted", "A") }, ""},
		{"archive inside another mirror's destination", func(j *Job) { j.Archive = smb("nas", "Backup", "Other/del") }, "Other"},
		{"log folder inside another mirror's destination", func(j *Job) { j.LogDir = smb("nas2", "backup", "other") }, "Other"},
		{"shared archive folder", func(j *Job) { j.Archive = smb("nas", "Deleted", "") }, "Keeper"},
		{"shared log folder is fine", func(j *Job) { j.LogDir = smb("nas", "Logs", "") }, ""},
		{"destination over another job's logs", func(j *Job) { j.Dest = *smb("nas", "Logs", "A") }, "Keeper"},
	}
	for _, tc := range cases {
		jj := j
		tc.edit(&jj)
		got := c.FolderConflict(jj)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: FolderConflict = %q, want %q", tc.name, got, tc.want)
		}
	}
}
