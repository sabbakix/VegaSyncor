package cloud

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vegasyncor/internal/config"
)

// The sync tests need rclone: VEGASYNCOR_TEST_RCLONE can point to the binary.
func needRclone(t *testing.T) {
	t.Helper()
	if b := os.Getenv("VEGASYNCOR_TEST_RCLONE"); b != "" {
		Binary = b
	}
	if _, err := exec.LookPath(Binary); err != nil {
		t.Skip("rclone not available")
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lines returns the itemized lines (changes) of a log.
func lines(log string) []string {
	var out []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, ">f") || strings.HasPrefix(l, "*deleting") {
			out = append(out, l)
		}
	}
	return out
}

func has(ls []string, want string) bool {
	for _, l := range ls {
		if l == want {
			return true
		}
	}
	return false
}

// setup: a source and a "drive" with a previous copy (a.txt older and different,
// gone.txt deleted at the source).
func setup(t *testing.T) (src, drive string) {
	src, drive = t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a.txt"), "new version")
	write(t, filepath.Join(src, "sub", "b.txt"), "b")
	write(t, filepath.Join(src, "Thumbs.db"), "x")
	write(t, filepath.Join(drive, "Backup", "a.txt"), "old")
	write(t, filepath.Join(drive, "Backup", "gone.txt"), "deleted at the source")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(drive, "Backup", "a.txt"), old, old)
	return
}

func TestSyncModes(t *testing.T) {
	needRclone(t)
	for _, mode := range config.Modes {
		t.Run(mode, func(t *testing.T) {
			src, drive := setup(t)
			cfg, err := NewConfig(t.TempDir(), Remote{DevRoot: drive}, "Backup", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cfg.Close()
			opts := Options{Src: src, Mode: mode, RunStamp: "2026-10-30_220000", DryRun: true}

			var log bytes.Buffer
			res, err := cfg.Sync(context.Background(), opts, &log, nil)
			if err != nil {
				t.Fatalf("dry run: %v\n%s", err, log.String())
			}
			ls := lines(log.String())
			if !has(ls, ">f+++++++++ sub/b.txt") || !has(ls, ">f.st...... a.txt") {
				t.Errorf("dry run lines: %v", ls)
			}
			if b, _ := os.ReadFile(filepath.Join(drive, "Backup", "a.txt")); string(b) != "old" {
				t.Fatal("the dry run changed the destination")
			}
			wantDel := mode != config.ModeAdditive
			if has(ls, "*deleting   gone.txt") != wantDel || res.Stats.FilesTransferred != 2 {
				t.Errorf("dry run: deletions %v, stats %+v, lines %v", wantDel, res.Stats, ls)
			}

			log.Reset()
			opts.DryRun = false
			res, err = cfg.Sync(context.Background(), opts, &log, nil)
			if err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			ls = lines(log.String())
			if !has(ls, ">f+++++++++ sub/b.txt") || !has(ls, ">f.st...... a.txt") || has(ls, ">f+++++++++ Thumbs.db") {
				t.Errorf("lines: %v", ls)
			}
			_, goneErr := os.Stat(filepath.Join(drive, "Backup", "gone.txt"))
			arch := filepath.Join(drive, "Backup", config.ArchiveDirName, "2026-10-30_220000")
			switch mode {
			case config.ModeAdditive:
				if goneErr != nil || has(ls, "*deleting   gone.txt") {
					t.Error("add only deleted a file")
				}
			case config.ModeMirror:
				if goneErr == nil || !has(ls, "*deleting   gone.txt") {
					t.Error("mirror did not delete")
				}
			case config.ModeMirrorArchive:
				if b, _ := os.ReadFile(filepath.Join(arch, "gone.txt")); string(b) != "deleted at the source" {
					t.Error("deleted file not archived")
				}
				if b, _ := os.ReadFile(filepath.Join(arch, "a.txt")); string(b) != "old" {
					t.Error("previous version not archived")
				}
				if !has(ls, "*deleting   gone.txt") {
					t.Errorf("archive: lines %v", ls)
				}
			}
			if res.Stats.FilesTransferred != 2 {
				t.Errorf("stats %+v", res.Stats)
			}
			// a second run changes nothing and leaves the archive alone
			log.Reset()
			res, err = cfg.Sync(context.Background(), opts, &log, nil)
			if err != nil || len(lines(log.String())) != 0 {
				t.Errorf("second run: %v %v", err, lines(log.String()))
			}
			if mode == config.ModeMirrorArchive {
				if _, err := os.Stat(arch); err != nil {
					t.Error("archive removed by the mirror")
				}
				removed, err := cfg.PruneArchive(context.Background(), cfg.Dest+"/"+config.ArchiveDirName, 1,
					time.Date(2026, 11, 5, 0, 0, 0, 0, time.Local))
				if err != nil || len(removed) != 1 {
					t.Errorf("prune: %v %v", removed, err)
				}
			}
		})
	}
}

func TestEncryptedSync(t *testing.T) {
	needRclone(t)
	src, drive := setup(t)
	os.RemoveAll(filepath.Join(drive, "Backup"))
	cfg, err := NewConfig(t.TempDir(), Remote{DevRoot: drive}, "Backup", &Crypt{Password: "correct horse", Names: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cfg.Close()
	var log bytes.Buffer
	if _, err := cfg.Sync(context.Background(), Options{Src: src, Mode: config.ModeMirrorArchive, RunStamp: "2026-10-30_220000"}, &log, nil); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if !has(lines(log.String()), ">f+++++++++ sub/b.txt") {
		t.Errorf("the log must show the real names: %v", lines(log.String()))
	}
	filepath.Walk(drive, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			raw, _ := os.ReadFile(p)
			if strings.Contains(p, "b.txt") || strings.Contains(string(raw), "new version") {
				t.Errorf("clear name or content on the drive: %s", p)
			}
		}
		return nil
	})
	dirs, err := cfg.ListDirs(context.Background(), cfg.Dest)
	if err != nil || len(dirs) != 1 || dirs[0] != "sub" {
		t.Errorf("decrypted listing: %v %v", dirs, err)
	}
}

func TestBrowseMkdirAbout(t *testing.T) {
	needRclone(t)
	drive := t.TempDir()
	write(t, filepath.Join(drive, "Backup", "x", "f"), "1")
	os.MkdirAll(filepath.Join(drive, "Backup", config.ArchiveDirName), 0o755)
	cfg, err := NewConfig(t.TempDir(), Remote{DevRoot: drive}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cfg.Close()
	if err := cfg.Mkdir(context.Background(), cfg.Abs("Backup/New folder")); err != nil {
		t.Fatal(err)
	}
	dirs, err := cfg.ListDirs(context.Background(), cfg.Abs("Backup"))
	if err != nil || strings.Join(dirs, ",") != "New folder,x" {
		t.Errorf("dirs = %v, %v", dirs, err)
	}
	if s, err := cfg.About(context.Background()); err != nil || s.Total == 0 {
		t.Errorf("about = %+v, %v", s, err)
	}
}

func TestTokenInConfig(t *testing.T) {
	needRclone(t)
	tok := `{"access_token":"a","token_type":"Bearer","refresh_token":"r","expiry":"2026-10-30T12:00:00Z"}`
	cfg, err := NewConfig(t.TempDir(), Remote{DriveID: "abc", DriveType: "personal", Token: tok}, "Backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cfg.Close()
	if cfg.Token() != tok || cfg.Dest != "od:Backup" {
		t.Errorf("token %q dest %q", cfg.Token(), cfg.Dest)
	}
	st, _ := os.Stat(cfg.path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("config file mode %v", st.Mode())
	}
	cfg.Close()
	if _, err := os.Stat(cfg.path); err == nil {
		t.Error("config file (with the token) not removed")
	}
	if _, err := NewConfig(t.TempDir(), Remote{Token: "x\ny"}, "", nil); err == nil {
		t.Error("token with a newline accepted")
	}
}

func TestCallbackAndToken(t *testing.T) {
	q, err := callbackQuery("http://localhost:53682/?code=M.C1_abc&state=xyz&session_state=1")
	if err != nil || q != "code=M.C1_abc&state=xyz" {
		t.Errorf("query = %q, %v", q, err)
	}
	if _, err := callbackQuery("http://localhost:53682/?error=access_denied&error_description=The+user+declined"); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("denied: %v", err)
	}
	if _, err := callbackQuery("hello"); err == nil {
		t.Error("garbage accepted")
	}
	out := "NOTICE: ...\nPaste the following into your remote machine --->\n{\"access_token\":\"A\",\"refresh_token\":\"R\"}\n<---End paste\n"
	if tok, err := parseToken(out); err != nil || !strings.Contains(tok, `"A"`) {
		t.Errorf("token = %q, %v", tok, err)
	}
	if _, err := parseToken(`2026/10/30 11:24:30 Fatal error: failed to get token: oauth2: "invalid_grant"`); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("fatal: %v", err)
	}
}

func TestDriveInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer A" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"InvalidAuthenticationToken"}}`))
			return
		}
		w.Write([]byte(`{"id":"b!xyz","driveType":"business","owner":{"user":{"displayName":"Stefy"}}}`))
	}))
	defer srv.Close()
	GraphURL = srv.URL
	d, err := DriveInfo(context.Background(), `{"access_token":"A"}`)
	if err != nil || d != (Drive{ID: "b!xyz", Type: "business", Owner: "Stefy"}) {
		t.Errorf("drive = %+v, %v", d, err)
	}
	if _, err := DriveInfo(context.Background(), `{"access_token":"B"}`); err == nil || !strings.Contains(err.Error(), "InvalidAuthenticationToken") {
		t.Errorf("bad token: %v", err)
	}
}

// The sign-in link comes from the local rclone listener; a pasted address of
// another sign-in is refused (Microsoft is not contacted in these cases).
func TestAuthLinkAndWrongState(t *testing.T) {
	needRclone(t)
	a, link, err := StartAuth(t.TempDir())
	if err != nil {
		t.Skipf("rclone authorize not usable here: %v", err)
	}
	defer a.Cancel()
	if !strings.HasPrefix(link, "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?") ||
		!strings.Contains(link, "offline_access") {
		t.Errorf("link = %s", link)
	}
	_, err = a.Finish(context.Background(), "http://localhost:53682/?code=X&state=WRONG")
	if err == nil || !strings.Contains(err.Error(), "another sign-in") {
		t.Errorf("wrong state: %v", err)
	}
}

// The browser can reach the rclone listener itself (same machine, WSL): then the
// result of rclone is used with nothing pasted. Here the fake code is refused by
// Microsoft, and that answer is what Finish must report.
func TestAuthCompletedByBrowser(t *testing.T) {
	needRclone(t)
	a, link, err := StartAuth(t.TempDir())
	if err != nil {
		t.Skipf("rclone authorize not usable here: %v", err)
	}
	defer a.Cancel()
	u, _ := url.Parse(link)
	resp, err := http.Get("http://" + ListenAddr + "/?code=FAKE&state=" + u.Query().Get("state"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case <-a.done:
	case <-time.After(30 * time.Second):
		t.Skip("no answer from Microsoft (offline?)")
	}
	_, err = a.Finish(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "sign-in failed") {
		t.Errorf("Finish = %v", err)
	}
}
