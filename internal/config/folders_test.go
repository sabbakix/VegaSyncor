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

func s3Config() *Config {
	return &Config{Connections: []Connection{
		{ID: "s1", Name: "S3", Kind: ConnS3, Region: "eu-west-1", Bucket: "backups", Username: "k"},
		{ID: "s2", Name: "S3 same bucket", Kind: ConnS3, Region: "eu-west-1", Bucket: "backups", Username: "k2"},
		{ID: "s3", Name: "S3 other", Kind: ConnS3, Endpoint: "https://s3.example.com", Bucket: "backups", Username: "k"},
		{ID: "od", Name: "OD", Kind: ConnOneDrive, DriveID: "d", DriveType: "personal"},
		{ID: "pc", Name: "PC", Host: "pc", Username: "u"},
	}}
}

func TestS3ConnectionValidate(t *testing.T) {
	good := Connection{Name: "N", Kind: ConnS3, Region: "eu-west-1", Bucket: "my-bucket", Username: " AKIA ", Host: "x", Domain: "y", DriveID: "z"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	if good.Username != "AKIA" || good.Host != "" || good.Domain != "" || good.DriveID != "" {
		t.Errorf("not cleaned: %+v", good)
	}
	cases := []struct {
		c  Connection
		ok bool
	}{
		{Connection{Name: "N", Kind: ConnS3, Bucket: "bb", Username: "k"}, false}, // AWS needs the region
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "s3.example.com", Bucket: "bb", Username: "k"}, true},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "http://10.0.0.1:9000/", Bucket: "bb", Username: "k"}, true},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "http://[fd00::1]:9000", Bucket: "bb", Username: "k"}, true},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "ftp://x", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "https://x/bucket", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "https://u:p@x", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "https://x?a=b", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Endpoint: "https://x\n[evil]", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "eu\nx", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "r", Bucket: "a/b", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "r", Bucket: "b\n", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "r", Bucket: "", Username: "k"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "r", Bucket: "bb"}, false},
		{Connection{Name: "N", Kind: ConnS3, Region: "r", Bucket: "bb", Username: "a\nb"}, false},
		{Connection{Name: "", Kind: ConnS3, Region: "r", Bucket: "bb", Username: "k"}, false},
		{Connection{Name: "N", Kind: "other"}, false},
	}
	for _, c := range cases {
		if err := c.c.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.c, err)
		}
	}
	smb := Connection{Name: "N", Host: "pc", Endpoint: "x", Bucket: "y", Region: "z"}
	if err := smb.Validate(); err != nil || smb.Endpoint+smb.Bucket+smb.Region != "" {
		t.Errorf("SMB with S3 fields: %+v %v", smb, err)
	}
	h, p, ok := (&Connection{Kind: ConnS3, Endpoint: "http://[fd00::1]:9000"}).S3HostPort()
	if h != "fd00::1" || p != "9000" || !ok {
		t.Errorf("host %q port %q %v", h, p, ok)
	}
	if _, p, _ := (&Connection{Kind: ConnS3, Endpoint: "http://x"}).S3HostPort(); p != "80" {
		t.Errorf("http port %q", p)
	}
	if _, p, _ := (&Connection{Kind: ConnS3, Endpoint: "https://x"}).S3HostPort(); p != "443" {
		t.Errorf("https port %q", p)
	}
	if _, _, ok := (&Connection{Kind: ConnS3}).S3HostPort(); ok {
		t.Error("AWS has no custom endpoint")
	}
}

func TestS3JobValidate(t *testing.T) {
	c := s3Config()
	s3 := func(id, p string) Location { return Location{Type: LocS3, ConnectionID: id, Path: p} }
	od := Location{Type: LocOneDrive, ConnectionID: "od", Path: "x"}
	local := Location{Type: LocLocal, Path: "/srv/data"}
	smb := Location{Type: LocSMB, ConnectionID: "pc", Share: "Docs"}
	job := func(src, dst Location, mode string) Job {
		return Job{Name: "J", Source: src, Dest: dst, Mode: mode, Schedule: Schedule{Type: SchedManual}}
	}
	check := func(what string, j Job, want string) {
		t.Helper()
		err := j.Validate(c)
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: %v", what, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: got %v, want %q", what, err, want)
		}
	}
	check("upload", job(local, s3("s1", "Backup"), ModeMirrorArchive), "")
	check("restore", job(s3("s1", "Backup"), local, ModeMirror), "")
	check("add only to the bucket root", job(local, s3("s1", ""), ModeAdditive), "")
	check("mirror to the bucket root", job(local, s3("s1", ""), ModeMirror), "needs a folder")
	check("mirror+archive to the bucket root", job(local, s3("s1", "/"), ModeMirrorArchive), "needs a folder")
	check("both cloud", job(s3("s1", "a"), s3("s3", "b"), ModeMirror), "cannot both be cloud")
	check("OneDrive to S3", job(od, s3("s1", "b"), ModeMirror), "cannot both be cloud")
	check("path out of the bucket", job(local, s3("s1", "../x"), ModeMirror), "cannot leave")
	check("S3 location on a SMB connection", job(local, s3("pc", "b"), ModeMirror), "select an S3 connection")
	check("S3 location on a OneDrive connection", job(local, s3("od", "b"), ModeMirror), "select an S3 connection")
	check("SMB location on an S3 connection", job(Location{Type: LocSMB, ConnectionID: "s1", Share: "D"}, s3("s1", "b"), ModeMirror), "select a connection")

	a := s3("s1", "Deleted")
	j := job(local, s3("s1", "Backup"), ModeMirrorArchive)
	j.Archive = &a
	check("archive in the same bucket", j, "")
	a2 := s3("s3", "Deleted")
	j.Archive = &a2
	check("archive in another bucket", j, "same account or bucket")
	j.Archive = &od
	check("archive on OneDrive", j, "same account or bucket")
	inDest := s3("s1", "Backup/Deleted")
	j.Archive = &inDest
	check("archive inside the destination", j, "")
	root := s3("s1", "")
	j.Archive = &root
	check("archive containing the destination", j, "cannot be the destination or contain it")
	j2 := job(smb, local, ModeMirrorArchive)
	j2.Archive = &a
	check("cloud archive of a local destination", j2, "only with a cloud destination")
	lg := s3("s1", "Logs")
	j3 := job(local, s3("s1", "Backup"), ModeMirror)
	j3.LogDir = &lg
	check("logs on S3", j3, "log folder")

	r := job(s3("s1", "Backup"), local, ModeMirror)
	r.AllowEmptySource = true
	if err := r.Validate(c); err != nil || r.AllowEmptySource || !r.SourceRO {
		t.Errorf("restore: %v, allow empty %v, read-only %v", err, r.AllowEmptySource, r.SourceRO)
	}
	e := job(local, s3("s1", "Backup"), ModeMirror)
	e.Encrypt, e.CryptPassword = true, "password"
	if err := e.Validate(c); err != nil || !e.Encrypt {
		t.Errorf("encryption: %v %v", err, e.Encrypt)
	}
	e.CryptPassword = ""
	check("encryption without password", e, "enter the password")
}

func TestS3Overlap(t *testing.T) {
	c := s3Config()
	s3 := func(id, p string) Location { return Location{Type: LocS3, ConnectionID: id, Path: p} }
	if !c.locationsOverlap(s3("s1", "Backup"), s3("s2", "Backup/x")) {
		t.Error("two connections to the same bucket must overlap")
	}
	if c.locationsOverlap(s3("s1", "Backup"), s3("s3", "Backup")) {
		t.Error("same bucket name on another service")
	}
	if c.locationsOverlap(s3("s1", "Backup"), s3("s1", "backup")) {
		t.Error("S3 keys are case-sensitive")
	}
	if c.locationsOverlap(s3("s1", "Back"), s3("s1", "Backup")) {
		t.Error("a name that only starts the same is another folder")
	}
	if !c.locationsOverlap(s3("s1", ""), s3("s1", "Backup")) {
		t.Error("the bucket root contains every folder")
	}
	jobs := []Job{{ID: "a", Name: "A", Mode: ModeMirror, Dest: s3("s1", "Backup")}}
	c.Jobs = jobs
	if c.DestConflict(Job{ID: "b", Mode: ModeMirror, Dest: s3("s2", "Backup/sub")}) == nil {
		t.Error("destination conflict not found")
	}
	if got := s3("s1", "Backup/x").Display(c); got != "s3://backups/Backup/x" {
		t.Errorf("display %q", got)
	}
}
