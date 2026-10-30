package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/cloud"
	"vegasyncor/internal/config"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/syncer"
)

// --- sign-in ---

// signIn is the OneDrive sign-in in progress: only one at a time, because
// "rclone authorize" listens on a fixed port.
type signIn struct {
	id    string
	auth  *cloud.Auth
	timer *time.Timer
}

var (
	signMu  sync.Mutex
	current *signIn
)

const signInTimeout = 15 * time.Minute

func devOneDrive() bool { return paths.DevMode() && paths.DevOneDriveRoot() != "" }

func (d *Daemon) handleOneDriveSignIn(w http.ResponseWriter, r *http.Request) {
	if devOneDrive() {
		// same form as the real address (rclone's public app ID); nothing is contacted
		reply(w, api.OneDriveStart{Session: "dev", URL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?access_type=offline" +
			"&client_id=b15665d9-eda6-4092-8539-0eec376afd59&redirect_uri=http%3A%2F%2Flocalhost%3A53682%2F&response_type=code" +
			"&scope=Files.Read+Files.ReadWrite+Files.Read.All+Files.ReadWrite.All+Sites.Read.All+offline_access&state=" + config.NewID() + config.NewID()})
		return
	}
	signMu.Lock()
	defer signMu.Unlock()
	if current != nil {
		current.timer.Stop()
		current.auth.Cancel()
		current = nil
	}
	a, link, err := cloud.StartAuth(paths.RuntimeDir())
	if err != nil {
		fail(w, 500, err)
		return
	}
	s := &signIn{id: config.NewID(), auth: a}
	s.timer = time.AfterFunc(signInTimeout, func() {
		signMu.Lock()
		defer signMu.Unlock()
		if current == s {
			s.auth.Cancel()
			current = nil
		}
	})
	current = s
	slog.Info("OneDrive sign-in started")
	reply(w, api.OneDriveStart{Session: s.id, URL: link})
}

func (d *Daemon) handleOneDriveFinish(w http.ResponseWriter, r *http.Request) {
	var in api.OneDriveFinishRequest
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if devOneDrive() {
		if !strings.Contains(in.Redirect, "code=") {
			fail(w, 400, errors.New(T("paste the whole address shown by the browser after the sign-in (it starts with http://localhost:53682/?code=)")))
			return
		}
		reply(w, api.OneDriveAccount{Token: `{"access_token":"dev","token_type":"Bearer","refresh_token":"dev","expiry":"2099-01-01T00:00:00Z"}`,
			DriveID: "dev-drive", DriveType: "business", Account: "Office Backup"})
		return
	}
	signMu.Lock()
	s := current
	signMu.Unlock()
	if s == nil || s.id != in.Session {
		fail(w, 409, errors.New(T("the sign-in is no longer active: start it again")))
		return
	}
	token, err := s.auth.Finish(ctx, in.Redirect)
	signMu.Lock()
	if current == s {
		s.timer.Stop()
		current = nil // the rclone listener has finished (or was stopped)
	}
	signMu.Unlock()
	if err != nil {
		fail(w, 400, err)
		return
	}
	drive, err := cloud.DriveInfo(ctx, token)
	if err != nil {
		fail(w, 502, err)
		return
	}
	slog.Info("OneDrive sign-in completed", "account", drive.Owner, "drive_type", drive.Type)
	reply(w, api.OneDriveAccount{Token: token, DriveID: drive.ID, DriveType: drive.Type, Account: drive.Owner})
}

// --- rclone configuration ---

// oneDrive prepares the rclone configuration for the drive of a connection; dest
// is the destination folder (the base of the encryption when crypt is set).
// release removes it and stores the token if rclone refreshed it.
func (d *Daemon) oneDrive(cfg *config.Config, connID, dest string, crypt *cloud.Crypt) (c *cloud.Config, release func(), err error) {
	cn := cfg.Connection(connID)
	if cn == nil || !cn.IsOneDrive() {
		return nil, nil, errors.New(Tf("connection %q not found", connID))
	}
	if err := cloud.Available(); err != nil {
		return nil, nil, err
	}
	token, err := d.box.Decrypt(cn.PasswordEnc, cn.ID)
	if err != nil {
		return nil, nil, err
	}
	if token == "" {
		return nil, nil, errors.New(Tf("OneDrive %s: not signed in (edit the connection and sign in)", cn.Name))
	}
	r := cloud.Remote{DriveID: cn.DriveID, DriveType: cn.DriveType, Token: token}
	if devOneDrive() {
		r.DevRoot = filepath.Join(paths.DevOneDriveRoot(), cn.DriveID)
		if err := os.MkdirAll(r.DevRoot, 0o755); err != nil {
			return nil, nil, err
		}
	}
	if err := os.MkdirAll(paths.RuntimeDir(), 0o755); err != nil {
		return nil, nil, err
	}
	c, err = cloud.NewConfig(paths.RuntimeDir(), r, dest, crypt)
	if err != nil {
		return nil, nil, err
	}
	return c, func() {
		if t := c.Token(); t != "" && t != token {
			d.storeToken(connID, t)
		}
		c.Close()
	}, nil
}

// storeToken saves a token refreshed by rclone (Microsoft gives a new refresh
// token at every refresh: the old one stops working after a while).
func (d *Daemon) storeToken(connID, token string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cn := d.cfg.Connection(connID)
	if cn == nil {
		return
	}
	enc, err := d.box.Encrypt(token, connID)
	if err != nil {
		slog.Error("OneDrive: cannot encrypt the refreshed token", "error", err)
		return
	}
	prev := cloneConfig(d.cfg)
	cn.PasswordEnc = enc
	if err := d.saveConfigLocked(prev); err != nil {
		slog.Error("OneDrive: cannot save the refreshed token", "error", err)
	}
}

// cryptOf returns the encryption settings of a job (nil if not encrypted).
func (d *Daemon) cryptOf(j config.Job) (*cloud.Crypt, error) {
	if !j.Encrypt {
		return nil, nil
	}
	pass, err := d.box.Decrypt(j.CryptPassEnc, config.CryptAAD(j.ID))
	if err != nil || pass == "" {
		return nil, errors.New(T("encryption: the password cannot be read (enter it again in the job)"))
	}
	return &cloud.Crypt{Password: pass, Names: j.EncryptNames}, nil
}

// --- runs ---

// syncOneDrive copies src to the OneDrive destination of j; it returns the
// phase to report when it fails.
func (d *Daemon) syncOneDrive(ctx context.Context, cfg *config.Config, j config.Job, r *api.Run, src string, lf io.Writer, logf func(string)) (syncer.Result, string, error) {
	d.setPhase(r, T("connecting to OneDrive"))
	c, release, err := d.oneDriveOf(cfg, j, j.Dest, logf)
	if err != nil {
		return syncer.Result{}, T("destination"), err
	}
	defer release()

	var protect []string
	for _, l := range []*config.Location{j.Archive, j.LogDir} {
		if l != nil {
			if rel, ok := cfg.RelPath(*l, j.Dest); ok && rel != "" {
				protect = append(protect, rel)
			}
		}
	}
	archive := cloud.Join(c.Root, config.ArchiveDirName)
	if j.Mode == config.ModeMirrorArchive && j.Archive != nil {
		archive = c.Abs(j.Archive.Path) // same drive (checked when the job is saved)
	}

	d.setPhase(r, T("synchronizing"))
	opts := cloud.Options{
		Src: src, Dst: c.Root, Mode: j.Mode, Excludes: j.Excludes, DryRun: r.DryRun, BandwidthKBps: j.BandwidthKBps,
		Checksum: j.Checksum, Protect: protect, RunStamp: r.Start.Format(syncer.StampFormat),
	}
	if j.Mode == config.ModeMirrorArchive {
		opts.Archive = archive
	}
	res, err := c.Sync(ctx, opts, lf, func(p syncer.Progress) {
		d.mu.Lock()
		r.Progress = &p
		d.mu.Unlock()
	})
	if err != nil {
		return res, "OneDrive", err
	}
	if j.Mode == config.ModeMirrorArchive && !r.DryRun && j.ArchiveDays > 0 {
		d.setPhase(r, T("cleaning up the archive"))
		removed, err := c.PruneArchive(ctx, archive, j.ArchiveDays, time.Now())
		if err != nil {
			logf(Tf("warning: archive cleanup: %v", err))
		} else if len(removed) > 0 {
			logf(Tf("archive: removed %d folders older than %d days", len(removed), j.ArchiveDays))
		}
	}
	return res, "", nil
}

// oneDriveOf prepares the OneDrive side of a job (source or destination, loc),
// with the encryption of the job, and writes the encryption mode to the log.
func (d *Daemon) oneDriveOf(cfg *config.Config, j config.Job, loc config.Location, logf func(string)) (*cloud.Config, func(), error) {
	crypt, err := d.cryptOf(j)
	if err != nil {
		return nil, nil, err
	}
	c, release, err := d.oneDrive(cfg, loc.ConnectionID, loc.Path, crypt)
	if err != nil {
		return nil, nil, err
	}
	if crypt != nil {
		names := T("contents only")
		if crypt.Names {
			names = T("contents and names")
		}
		logf(Tf("Encryption:  on (%s)", names))
	}
	return c, release, nil
}

// --- connection test, browsing ---

func (d *Daemon) testOneDrive(ctx context.Context, cfg *config.Config, id string) api.TestResult {
	c, release, err := d.oneDrive(cfg, id, "", nil)
	if err != nil {
		return api.TestResult{OK: false, Message: err.Error()}
	}
	defer release()
	s, err := c.About(ctx)
	if err != nil {
		return api.TestResult{OK: false, Message: err.Error()}
	}
	return api.TestResult{OK: true, Message: Tf("access granted: %s free of %s", api.HumanBytes(s.Free), api.HumanBytes(s.Total))}
}

func (d *Daemon) browseOneDrive(ctx context.Context, loc config.Location) (api.BrowseResponse, error) {
	p, err := config.CleanSubPath(loc.Path)
	if err != nil {
		return api.BrowseResponse{}, err
	}
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	c, release, err := d.oneDrive(cfg, loc.ConnectionID, "", nil)
	if err != nil {
		return api.BrowseResponse{}, err
	}
	defer release()
	dirs, err := c.ListDirs(ctx, c.Abs(p))
	if err != nil {
		return api.BrowseResponse{}, err
	}
	sort.Slice(dirs, func(a, b int) bool { return strings.ToLower(dirs[a]) < strings.ToLower(dirs[b]) })
	return api.BrowseResponse{Path: p, Dirs: dirs}, nil
}

func (d *Daemon) mkdirOneDrive(ctx context.Context, loc config.Location, name string) (string, error) {
	p, err := config.CleanSubPath(loc.Path)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	c, release, err := d.oneDrive(cfg, loc.ConnectionID, "", nil)
	if err != nil {
		return "", err
	}
	defer release()
	dirs, err := c.ListDirs(ctx, c.Abs(p))
	if err != nil {
		return "", err
	}
	for _, x := range dirs {
		if strings.EqualFold(x, name) {
			return "", errAlreadyExists
		}
	}
	newPath := strings.TrimPrefix(path.Join(p, name), "/")
	return newPath, c.Mkdir(ctx, c.Abs(newPath))
}

var errAlreadyExists = errors.New("exists")
