package tui

import (
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone"

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

// The preview box highlighted while editing a job follows the section of the field.
func TestJobFormSections(t *testing.T) {
	fm := newJobForm(nil, nil, "")
	want := map[string]string{
		"name": secGeneral, "enabled": secGeneral,
		"src_type": secSource, "src_share": secSource, "src_ro": secSource,
		"dst_type": secDest, "dst_path": secDest, "src_odconn": secSource, "dst_odconn": secDest,
		"encrypt": secCrypt, "crypt_pass": secCrypt,
		"mode": secCopy, "arc_type": secCopy, "checksum": secCopy, "log_days": secCopy,
		"sched": secSchedule, "times": secSchedule,
	}
	for i, f := range fm.Fields {
		if sec, ok := want[f.Key]; ok {
			if got := fm.sectionOf(i); got != sec {
				t.Errorf("field %s: section %q, want %q", f.Key, got, sec)
			}
			delete(want, f.Key)
		}
	}
	if len(want) > 0 {
		t.Errorf("fields not found: %v", want)
	}
}

func s3Conns() []api.ConnectionView {
	return []api.ConnectionView{
		{Connection: config.Connection{ID: "pc", Name: "PC", Host: "PC"}},
		{Connection: config.Connection{ID: "od", Name: "OD", Kind: config.ConnOneDrive, DriveID: "d", DriveType: "personal"}},
		{Connection: config.Connection{ID: "s1", Name: "Bucket", Kind: config.ConnS3, Bucket: "backups", Region: "eu", Username: "k"}},
	}
}

func choose(t *testing.T, fm *form, key, value string) {
	t.Helper()
	f := fm.get(key)
	for i, o := range f.Options {
		if o.Value == value {
			f.Sel = i
			return
		}
	}
	t.Fatalf("%s has no option %q", key, value)
}

// The lists of connections hold only those of their kind: an S3 bucket is not a PC.
func TestConnectionLists(t *testing.T) {
	conns := s3Conns()
	if o := connOptions(conns); len(o) != 1 || o[0].Value != "pc" {
		t.Errorf("SMB: %+v", o)
	}
	if o := oneDriveOptions(conns); len(o) != 1 || o[0].Value != "od" {
		t.Errorf("OneDrive: %+v", o)
	}
	if o := s3Options(conns); len(o) != 1 || o[0].Value != "s1" {
		t.Errorf("S3: %+v", o)
	}
	if o := s3Options(conns[:2]); len(o) != 1 || o[0].Value != "" {
		t.Errorf("no S3: %+v", o)
	}
	if firstConn(conns[1:]) != "" {
		t.Error("firstConn returned a cloud connection")
	}
}

func TestJobFormS3(t *testing.T) {
	fm := newJobForm(nil, s3Conns(), "")
	fm.get("name").setValue("Job")
	choose(t, fm, "src_type", config.LocLocal)
	fm.get("src_path").setValue("/srv/data")
	choose(t, fm, "dst_type", config.LocS3)
	fm.get("dst_path").setValue("Backup/X")
	choose(t, fm, "mode", config.ModeMirrorArchive)
	choose(t, fm, "arc_type", config.LocS3)
	fm.get("arc_path").setValue("Deleted")
	j, err := jobFromForm(fm, "")
	if err != nil {
		t.Fatal(err)
	}
	if j.Dest != (config.Location{Type: config.LocS3, ConnectionID: "s1", Path: "Backup/X"}) {
		t.Errorf("dest %+v", j.Dest)
	}
	if j.Archive == nil || *j.Archive != (config.Location{Type: config.LocS3, ConnectionID: "s1", Path: "Deleted"}) {
		t.Errorf("archive %+v", j.Archive)
	}

	// restore: the source is read-only whatever the hidden field holds
	fm = newJobForm(nil, s3Conns(), "")
	fm.get("name").setValue("Restore")
	fm.get("src_ro").Bool = false
	choose(t, fm, "src_type", config.LocS3)
	fm.get("src_path").setValue("Backup")
	choose(t, fm, "dst_type", config.LocLocal)
	fm.get("dst_path").setValue("/srv/restore")
	if fm.visible(fm.get("allow_empty")) || fm.visible(fm.get("src_ro")) {
		t.Error("options that do not apply are shown")
	}
	if !fm.visible(fm.get("encrypt")) || !fm.visible(fm.get("src_s3conn")) || fm.visible(fm.get("dst_s3conn")) {
		t.Error("S3 fields not shown as expected")
	}
	if j, err = jobFromForm(fm, ""); err != nil || !j.SourceRO || j.Source.ConnectionID != "s1" {
		t.Errorf("restore: %+v, %v", j, err)
	}
}

// Editing a job shows its bucket, and saving without changes gives the same job.
func TestJobFormS3Edit(t *testing.T) {
	orig := config.Job{ID: "j", Name: "Job", Enabled: true, SourceRO: true, Source: config.Location{Type: config.LocSMB, ConnectionID: "pc", Share: "Docs"},
		Dest: config.Location{Type: config.LocS3, ConnectionID: "s1", Path: "Backup"}, Mode: config.ModeMirror,
		Encrypt: true, EncryptNames: true, CryptPassEnc: "x", Schedule: config.Schedule{Type: config.SchedManual}}
	j, err := jobFromForm(newJobForm(&orig, s3Conns(), ""), "j")
	if err != nil || j.Dest != orig.Dest || j.Source != orig.Source || !j.Encrypt || !j.EncryptNames {
		t.Errorf("%+v, %v", j, err)
	}
}

// The preview follows every cloud type of the deleted items folder (it used to
// look for a field that does not exist, and crash).
func TestJobPreviewCloudArchive(t *testing.T) {
	for _, typ := range []string{config.LocOneDrive, config.LocS3} {
		m := &Model{w: 120, h: 40, st: &api.Status{Connections: s3Conns()}}
		m.form = newJobForm(nil, s3Conns(), "")
		choose(t, m.form, "dst_type", typ)
		choose(t, m.form, "mode", config.ModeMirrorArchive)
		choose(t, m.form, "arc_type", typ)
		m.form.get("arc_path").setValue("Deleted")
		if out := m.jobPreview(); !strings.Contains(out, "Deleted") {
			t.Errorf("%s: preview without the folder:\n%s", typ, out)
		}
	}
}

func TestConnFormS3(t *testing.T) {
	fm := newConnForm(nil)
	choose(t, fm, "kind", config.ConnS3)
	if fm.visible(fm.get("host")) || !fm.visible(fm.get("s3_bucket")) {
		t.Error("fields of the wrong kind shown")
	}
	fm.get("name").setValue("Off-site")
	fm.get("s3_endpoint").setValue("https://s3.example.com")
	fm.get("s3_region").setValue("eu-west-1")
	fm.get("s3_bucket").setValue("backups")
	fm.get("s3_key").setValue("AKIA")
	fm.get("s3_secret").setValue("  se/cret+\n")
	in := connFromForm(fm, "", nil)
	if !in.IsS3() || in.Endpoint != "https://s3.example.com" || in.Region != "eu-west-1" || in.Bucket != "backups" || in.Username != "AKIA" {
		t.Errorf("%+v", in)
	}
	if err := in.Validate(); err != nil {
		t.Error(err)
	}
	// editing: nothing is lost, an empty secret keeps the saved one
	v := &api.ConnectionView{Connection: in.Connection, HasPassword: true}
	in2 := connFromForm(newConnForm(v), "id", nil)
	if in2.Password != "" || in2.Bucket != "backups" || in2.Endpoint != in.Endpoint || in2.Region != in.Region || in2.Username != "AKIA" || !in2.IsS3() {
		t.Errorf("edit: %+v", in2)
	}
	if k := newConnForm(v).get("kind"); len(k.Options) != 1 {
		t.Errorf("the kind of a connection must not change: %+v", k.Options)
	}
}

// The screens with S3 connections and jobs can be drawn.
func TestS3Screens(t *testing.T) {
	zone.NewGlobal()
	v := s3Conns()
	st := &api.Status{Connections: v, Jobs: []api.JobStatus{{Job: config.Job{ID: "j", Name: "Job", Encrypt: true,
		Source: config.Location{Type: config.LocLocal, Path: "/srv"}, Dest: config.Location{Type: config.LocS3, ConnectionID: "s1", Path: "Backup"}, Mode: config.ModeMirror},
		Source: "/srv", Dest: "s3://backups/Backup"}}}
	m := &Model{w: 120, h: 40, st: st}
	out, _ := m.viewConns()
	if !strings.Contains(out, "Amazon S3") || !strings.Contains(out, "backups") {
		t.Errorf("connections:\n%s", out)
	}
	fm := newConnForm(nil)
	choose(t, fm, "kind", config.ConnS3)
	if out := fm.render(100, 0); !strings.Contains(out, T("Bucket")) || !strings.Contains(out, T("Secret key")) {
		t.Errorf("connection form:\n%s", out)
	}
	jf := newJobForm(nil, v, "")
	choose(t, jf, "dst_type", config.LocS3)
	if out := jf.render(100, 0); !strings.Contains(out, T("S3 connection")) || !strings.Contains(out, T("Folder in the bucket")) {
		t.Errorf("job form:\n%s", out)
	}
	b := &browser{loc: config.Location{Type: config.LocS3, Path: "Backup"}}
	if b.displayPath() != "/Backup" {
		t.Errorf("browser path %q", b.displayPath())
	}
}
