package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/cloud"
	"vegasyncor/internal/config"
	"vegasyncor/internal/firewall"
	"vegasyncor/internal/s3test"
)

const (
	s3Key    = "AKIATESTKEY"
	s3Secret = "wJalr/XUtnFEMI+K7MDENG=bPxRfiCY#key"
)

// s3Daemon is a service with an S3 connection "s3" saved through the API, like the TUI does.
func s3Daemon(t *testing.T) (*Daemon, *s3test.Server) {
	t.Helper()
	if _, err := exec.LookPath("rclone"); err != nil {
		t.Skip("rclone not available")
	}
	d, _ := newTestDaemon(t)
	srv := s3test.New(s3Key, s3Secret, "backups")
	t.Cleanup(srv.Close)
	copyR, checkR, sleep := cloud.S3CopyRetries, cloud.S3CheckRetries, cloud.S3RetriesSleep
	cloud.S3CopyRetries, cloud.S3CheckRetries, cloud.S3RetriesSleep = "1", "1", "0s"
	t.Cleanup(func() { cloud.S3CopyRetries, cloud.S3CheckRetries, cloud.S3RetriesSleep = copyR, checkR, sleep })
	id := saveS3Conn(t, d, api.ConnectionInput{Connection: config.Connection{Name: "Off-site", Kind: config.ConnS3,
		Endpoint: srv.URL, Bucket: "backups", Username: s3Key}, Password: s3Secret + "\n"})
	d.mu.Lock()
	d.cfg.Connection(id).ID = "s3"
	d.cfg.Connections[0].PasswordEnc, _ = d.box.Encrypt(s3Secret, "s3")
	d.mu.Unlock()
	return d, srv
}

func saveS3Conn(t *testing.T, d *Daemon, in api.ConnectionInput) string {
	t.Helper()
	w := call(d.handleSaveConn, http.MethodPost, in)
	if w.Code != 200 {
		t.Fatalf("save connection: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), s3Secret) {
		t.Error("the secret came back from the API")
	}
	var v api.ConnectionView
	json.Unmarshal(w.Body.Bytes(), &v)
	return v.ID
}

func s3Job(mode string, src, dst config.Location) config.Job {
	return config.Job{ID: "j1", Name: "Job", Source: src, SourceRO: true, Dest: dst, Mode: mode, Schedule: config.Schedule{Type: config.SchedManual}}
}

func s3Loc(p string) config.Location {
	return config.Location{Type: config.LocS3, ConnectionID: "s3", Path: p}
}
func localLoc(p string) config.Location {
	return config.Location{Type: config.LocLocal, Path: p}
}

// runJob validates the job like saving it does, runs it and returns status and message.
func runJob(t *testing.T, d *Daemon, j config.Job) (string, string) {
	t.Helper()
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	if err := j.Validate(cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if j.CryptPassword != "" {
		j.CryptPassEnc, _ = d.box.Encrypt(j.CryptPassword, config.CryptAAD(j.ID))
	}
	r := &api.Run{ID: "run-" + config.NewID(), JobID: j.ID, Start: time.Now()}
	status, msg := d.doExecute(context.Background(), cfg, j, r)
	if log, err := os.ReadFile(logPath(r.ID)); err == nil {
		t.Log(string(log))
		if strings.Contains(string(log), s3Secret) {
			t.Error("the secret is in the log")
		}
	}
	return status, msg
}

func put(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestS3SecretEncryptedInConfig(t *testing.T) {
	d, _ := s3Daemon(t)
	raw, err := os.ReadFile(os.Getenv("VEGASYNCOR_CONFIG_DIR") + "/config.json")
	if err != nil {
		matches, _ := filepath.Glob(os.Getenv("VEGASYNCOR_CONFIG_DIR") + "/*")
		t.Fatalf("%v (%v)", err, matches)
	}
	if strings.Contains(string(raw), s3Secret) {
		t.Error("the secret is readable in the configuration file")
	}
	if pw, err := d.box.Decrypt(d.cfg.Connection("s3").PasswordEnc, "s3"); err != nil || pw != s3Secret {
		t.Errorf("secret = %q, %v", pw, err)
	}
	// no secret: refused
	w := call(d.handleSaveConn, http.MethodPost, api.ConnectionInput{Connection: config.Connection{Name: "X", Kind: config.ConnS3,
		Endpoint: "https://s3.example.com", Bucket: "bb", Username: "k"}})
	if w.Code != 400 {
		t.Errorf("saved without a secret: %d", w.Code)
	}
}

// Backup, then restore into an empty folder: the whole way a user goes through.
func TestS3BackupAndRestore(t *testing.T) {
	d, srv := s3Daemon(t)
	src := t.TempDir()
	put(t, src+"/Contabilità/fattura 1.txt", "fattura")
	put(t, src+"/gone.txt", "old file")
	st, msg := runJob(t, d, s3Job(config.ModeMirrorArchive, localLoc(src), s3Loc("Backup")))
	if st != api.StatusOK || !strings.Contains(msg, "2 files copied") {
		t.Fatalf("backup: %s %s", st, msg)
	}
	if srv.Get("backups", "Backup/Contabilità/fattura 1.txt") == nil {
		t.Fatalf("keys: %v", srv.Keys("backups"))
	}
	os.Remove(src + "/gone.txt")
	st, msg = runJob(t, d, s3Job(config.ModeMirrorArchive, localLoc(src), s3Loc("Backup")))
	if st != api.StatusOK || !strings.Contains(msg, "1 deleted") {
		t.Fatalf("second backup: %s %s", st, msg)
	}
	found := false
	for _, k := range srv.Keys("backups") {
		found = found || (strings.Contains(k, config.ArchiveDirName) && strings.HasSuffix(k, "/gone.txt"))
	}
	if !found || srv.Get("backups", "Backup/gone.txt") != nil {
		t.Errorf("deleted file not archived: %v", srv.Keys("backups"))
	}

	restore := t.TempDir()
	st, msg = runJob(t, d, s3Job(config.ModeMirror, s3Loc("Backup"), localLoc(restore)))
	if st != api.StatusOK {
		t.Fatalf("restore: %s %s", st, msg)
	}
	if b, _ := os.ReadFile(restore + "/Contabilità/fattura 1.txt"); string(b) != "fattura" {
		t.Errorf("restored %q", b)
	}
	if _, err := os.Stat(restore + "/" + config.ArchiveDirName); err == nil {
		t.Error("the archive was restored with the files")
	}
}

// Encrypted names and the wrong password: the service recognises it and stops.
func TestS3EncryptedJob(t *testing.T) {
	d, srv := s3Daemon(t)
	src := t.TempDir()
	put(t, src+"/private.txt", "very private")
	j := s3Job(config.ModeMirrorArchive, localLoc(src), s3Loc("Backup"))
	j.Encrypt, j.EncryptNames, j.CryptPassword = true, true, "correct horse"
	if st, msg := runJob(t, d, j); st != api.StatusOK {
		t.Fatalf("%s %s", st, msg)
	}
	for _, k := range srv.Keys("backups") {
		if strings.Contains(k, "private") || strings.Contains(string(srv.Get("backups", k).Data), "very private") {
			t.Errorf("clear name or content in the bucket: %s", k)
		}
	}
	restore := t.TempDir()
	put(t, restore+"/local.txt", "must survive")
	r := s3Job(config.ModeMirror, s3Loc("Backup"), localLoc(restore))
	r.Encrypt, r.EncryptNames, r.CryptPassword = true, true, "wrong password"
	st, msg := runJob(t, d, r)
	if st != api.StatusError || !strings.Contains(msg, "nothing can be decrypted") {
		t.Errorf("wrong password: %s %s", st, msg)
	}
	if _, err := os.Stat(restore + "/local.txt"); err != nil {
		t.Error("the local copy was touched")
	}
	r.CryptPassword = "correct horse"
	if st, msg := runJob(t, d, r); st != api.StatusOK {
		t.Fatalf("right password: %s %s", st, msg)
	}
	if b, _ := os.ReadFile(restore + "/private.txt"); string(b) != "very private" {
		t.Errorf("restored %q", b)
	}
}

// A mistyped restore path stops in every mode, and the local copy is left alone.
func TestS3RestoreFromNothing(t *testing.T) {
	d, srv := s3Daemon(t)
	srv.Put("backups", "Backup/a.txt", []byte("x"))
	for _, mode := range config.Modes {
		restore := t.TempDir()
		put(t, restore+"/local.txt", "must survive")
		j := s3Job(mode, s3Loc("Bakcup"), localLoc(restore))
		j.AllowEmptySource = true // not even with this
		st, msg := runJob(t, d, j)
		if st != api.StatusError || !strings.Contains(msg, "no files found") {
			t.Errorf("%s: %s %s", mode, st, msg)
		}
		if _, err := os.Stat(restore + "/local.txt"); err != nil {
			t.Errorf("%s: local file removed", mode)
		}
	}
}

// An empty local source does not wipe the copy in the bucket.
func TestS3EmptySourceBlocked(t *testing.T) {
	d, srv := s3Daemon(t)
	srv.Put("backups", "Backup/a.txt", []byte("the backup"))
	st, msg := runJob(t, d, s3Job(config.ModeMirror, localLoc(t.TempDir()), s3Loc("Backup")))
	if st != api.StatusError || !strings.Contains(msg, "source is empty") || srv.Get("backups", "Backup/a.txt") == nil {
		t.Errorf("%s %s, keys %v", st, msg, srv.Keys("backups"))
	}
}

// Wrong bucket or keys stop the run before anything is copied, with a clear
// message, and the connection test says the same.
func TestS3BadConnection(t *testing.T) {
	d, srv := s3Daemon(t)
	src := t.TempDir()
	put(t, src+"/a.txt", "x")
	if res := d.testS3(context.Background(), d.cfg, "s3"); !res.OK {
		t.Errorf("test: %+v", res)
	}

	d.cfg.Connection("s3").Bucket = "typo"
	st, msg := runJob(t, d, s3Job(config.ModeMirror, localLoc(src), s3Loc("Backup")))
	if st != api.StatusError || !strings.Contains(msg, "the S3 bucket typo does not exist") {
		t.Errorf("bucket: %s %s", st, msg)
	}
	if res := d.testS3(context.Background(), d.cfg, "s3"); res.OK || !strings.Contains(res.Message, "does not exist") {
		t.Errorf("test: %+v", res)
	}
	if srv.HasBucket("typo") {
		t.Error("bucket created")
	}

	d.cfg.Connection("s3").Bucket = "backups"
	d.cfg.Connection("s3").PasswordEnc, _ = d.box.Encrypt("wrong", "s3")
	st, msg = runJob(t, d, s3Job(config.ModeMirror, localLoc(src), s3Loc("Backup")))
	if st != api.StatusError || !strings.Contains(msg, "SignatureDoesNotMatch") || strings.Contains(msg, "status code") {
		t.Errorf("secret: %s %s", st, msg)
	}
	if res := d.testS3(context.Background(), d.cfg, "s3"); res.OK {
		t.Errorf("test: %+v", res)
	}
	if len(srv.Keys("backups")) != 0 {
		t.Errorf("copied: %v", srv.Keys("backups"))
	}
}

// One failed file: the run is not reported as a success and nothing is deleted.
func TestS3PartialFailureReported(t *testing.T) {
	d, srv := s3Daemon(t)
	srv.Put("backups", "Backup/gone.txt", []byte("only copy"))
	srv.FailPut = func(k string) bool { return strings.HasSuffix(k, "bad.txt") }
	src := t.TempDir()
	put(t, src+"/good.txt", "ok")
	put(t, src+"/bad.txt", "ko")
	st, msg := runJob(t, d, s3Job(config.ModeMirror, localLoc(src), s3Loc("Backup")))
	if st == api.StatusOK {
		t.Errorf("reported as OK: %s", msg)
	}
	if srv.Get("backups", "Backup/gone.txt") == nil {
		t.Error("deleted after a failed upload")
	}
}

func TestS3BrowseAndMkdir(t *testing.T) {
	d, srv := s3Daemon(t)
	srv.Put("backups", "Backup/x/f.txt", []byte("1"))
	srv.Put("backups", "Backup/"+config.ArchiveDirName+"/2026/f.txt", []byte("1"))
	resp, err := d.browseCloud(context.Background(), s3Loc("Backup"))
	if err != nil || strings.Join(resp.Dirs, ",") != "x" {
		t.Errorf("browse: %+v %v", resp, err)
	}
	p, err := d.mkdirCloud(context.Background(), s3Loc("Backup"), "New")
	if err != nil || p != "Backup/New" {
		t.Errorf("mkdir: %q %v", p, err)
	}
	if _, err := d.mkdirCloud(context.Background(), s3Loc("Backup"), "x"); err != errAlreadyExists {
		t.Errorf("existing folder: %v", err)
	}
}

// The firewall lets through what the S3 connections need, and nothing as SMB.
func TestS3Firewall(t *testing.T) {
	d, _ := newTestDaemon(t)
	set := func(conns ...config.Connection) {
		d.mu.Lock()
		d.cfg.Connections = conns
		d.mu.Unlock()
	}
	s3 := func(endpoint string) config.Connection {
		return config.Connection{ID: endpoint, Name: "N", Kind: config.ConnS3, Endpoint: endpoint, Bucket: "bb", Username: "k"}
	}
	set(s3(""))
	smb, ep := d.connHosts(true)
	if len(smb) != 0 || len(ep) != 0 || !d.usesCloud() {
		t.Errorf("AWS: smb %v endpoints %v cloud %v", smb, ep, d.usesCloud())
	}
	set(s3("https://192.0.2.1"))
	smb, ep = d.connHosts(true)
	if len(smb) != 0 || len(ep) != 0 || !d.usesCloud() {
		t.Errorf("HTTPS: smb %v endpoints %v cloud %v", smb, ep, d.usesCloud())
	}
	set(s3("http://192.0.2.1:9000"), s3("https://192.0.2.2:9000"), s3("http://192.0.2.3"),
		config.Connection{ID: "pc", Name: "PC", Host: "192.0.2.9"})
	smb, ep = d.connHosts(true)
	if strings.Join(smb, ",") != "192.0.2.9" {
		t.Errorf("an S3 server was allowed as SMB: %v", smb)
	}
	want := []firewall.Endpoint{{Port: "80", Hosts: []string{"192.0.2.3"}}, {Port: "9000", Hosts: []string{"192.0.2.1", "192.0.2.2"}}}
	got, _ := json.Marshal(ep)
	wantJ, _ := json.Marshal(want)
	if string(got) != string(wantJ) {
		t.Errorf("endpoints %s, want %s", got, wantJ)
	}
	if d.usesCloud() {
		t.Error("HTTPS to any host opened for servers on other ports")
	}
	script := d.fwScript(config.Firewall{Enabled: true}, true)
	if !strings.Contains(script, "ip daddr { 192.0.2.1, 192.0.2.2 } tcp dport 9000") || !strings.Contains(script, "ip daddr 192.0.2.3 tcp dport 80") {
		t.Errorf("script:\n%s", script)
	}
}
