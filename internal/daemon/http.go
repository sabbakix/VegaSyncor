package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
)

func (d *Daemon) serveAPI() (*http.Server, error) {
	sock := paths.Socket()
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		return nil, err
	}
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	// access: root and (if it exists) the "vegasyncor" group
	mode := os.FileMode(0o600)
	if g, err := user.LookupGroup("vegasyncor"); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil && os.Chown(sock, -1, gid) == nil {
			mode = 0o660
		}
	}
	if err := os.Chmod(sock, mode); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { reply(w, d.status()) })
	mux.HandleFunc("POST /api/settings", d.handleSettings)
	mux.HandleFunc("GET /api/firewall", d.handleFirewallGet)
	mux.HandleFunc("PUT /api/firewall", d.handleFirewallSet)
	mux.HandleFunc("POST /api/firewall/confirm", d.handleFirewallConfirm)
	mux.HandleFunc("POST /api/firewall/revert", d.handleFirewallRevert)

	mux.HandleFunc("POST /api/jobs", d.handleSaveJob)
	mux.HandleFunc("PUT /api/jobs/{id}", d.handleSaveJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", d.handleDeleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/enabled", d.handleEnableJob)
	mux.HandleFunc("POST /api/jobs/{id}/run", d.handleRunJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", d.handleCancelJob)

	mux.HandleFunc("POST /api/connections", d.handleSaveConn)
	mux.HandleFunc("PUT /api/connections/{id}", d.handleSaveConn)
	mux.HandleFunc("DELETE /api/connections/{id}", d.handleDeleteConn)
	mux.HandleFunc("POST /api/connections/{id}/test", d.handleTestConn)

	mux.HandleFunc("POST /api/browse", d.handleBrowse)
	mux.HandleFunc("POST /api/mkdir", d.handleMkdir)
	mux.HandleFunc("GET /api/history", d.handleHistory)
	mux.HandleFunc("GET /api/runs/{id}/log", d.handleLog)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API", "error", err)
		}
	}()
	return srv, nil
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// saveConfigLocked saves the configuration; on error it restores the previous one.
func (d *Daemon) saveConfigLocked(prev *config.Config) error {
	if err := d.cfg.Save(paths.ConfigFile()); err != nil {
		d.cfg = prev
		return fmt.Errorf(T("saving the configuration: %w"), err)
	}
	return nil
}

func cloneConfig(c *config.Config) *config.Config {
	cp := *c
	cp.Connections = append([]config.Connection(nil), c.Connections...)
	cp.Jobs = append([]config.Job(nil), c.Jobs...)
	return &cp
}

// --- settings ---

func (d *Daemon) handleSettings(w http.ResponseWriter, r *http.Request) {
	var in api.Settings
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	if !i18n.Supported(in.Language) {
		fail(w, 400, errors.New(Tf("unsupported language: %s", in.Language)))
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := cloneConfig(d.cfg)
	d.cfg.Language = in.Language
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	i18n.SetLang(in.Language)
	slog.Info("language changed", "language", in.Language)
	reply(w, map[string]bool{"ok": true})
}

// --- job ---

func (d *Daemon) handleSaveJob(w http.ResponseWriter, r *http.Request) {
	var j config.Job
	if err := decode(r, &j); err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := cloneConfig(d.cfg)
	if id := r.PathValue("id"); id != "" {
		j.ID = id
		if d.cfg.Job(id) == nil {
			fail(w, 404, errors.New(T("job not found")))
			return
		}
	} else {
		j.ID = config.NewID()
	}
	if err := j.Validate(d.cfg); err != nil {
		fail(w, 400, err)
		return
	}
	if other := d.cfg.DestConflict(j); other != nil {
		fail(w, 400, errors.New(Tf("the destination overlaps with the job %q: a mirror would delete the other job's files", other.Name)))
		return
	}
	for _, o := range d.cfg.Jobs {
		if o.ID != j.ID && strings.EqualFold(o.Name, j.Name) {
			fail(w, 400, errors.New(T("a job with this name already exists")))
			return
		}
	}
	if existing := d.cfg.Job(j.ID); existing != nil {
		*existing = j
	} else {
		d.cfg.Jobs = append(d.cfg.Jobs, j)
	}
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	delete(d.next, j.ID) // recompute the next run
	slog.Info("job saved", "job", j.Name, "id", j.ID)
	reply(w, j)
}

func (d *Daemon) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, busy := d.running[id]; busy {
		fail(w, 409, errors.New(T("the job is running: stop it before deleting it")))
		return
	}
	prev := cloneConfig(d.cfg)
	out := d.cfg.Jobs[:0:0]
	for _, j := range d.cfg.Jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	if len(out) == len(d.cfg.Jobs) {
		fail(w, 404, errors.New(T("job not found")))
		return
	}
	d.cfg.Jobs = out
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	delete(d.next, id)
	reply(w, map[string]bool{"ok": true})
}

func (d *Daemon) handleEnableJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := cloneConfig(d.cfg)
	j := d.cfg.Job(r.PathValue("id"))
	if j == nil {
		fail(w, 404, errors.New(T("job not found")))
		return
	}
	j.Enabled = in.Enabled
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	delete(d.next, j.ID)
	reply(w, map[string]bool{"ok": true})
}

func (d *Daemon) handleRunJob(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	j := d.cfg.Job(r.PathValue("id"))
	if j == nil {
		fail(w, 404, errors.New(T("job not found")))
		return
	}
	run, err := d.startLocked(*j, r.URL.Query().Get("dry") == "1", api.TriggerManual)
	if err != nil {
		fail(w, 409, err)
		return
	}
	reply(w, run)
}

func (d *Daemon) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rj, ok := d.running[r.PathValue("id")]
	if !ok {
		fail(w, 409, errors.New(T("the job is not running")))
		return
	}
	rj.run.Phase = T("cancelling…")
	rj.cancel()
	reply(w, map[string]bool{"ok": true})
}

// --- connections ---

func (d *Daemon) handleSaveConn(w http.ResponseWriter, r *http.Request) {
	var in api.ConnectionInput
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := cloneConfig(d.cfg)
	c := in.Connection
	var existing *config.Connection
	if id := r.PathValue("id"); id != "" {
		if existing = d.cfg.Connection(id); existing == nil {
			fail(w, 404, errors.New(T("connection not found")))
			return
		}
		c.ID = id
		c.PasswordEnc = existing.PasswordEnc
	} else {
		c.ID = config.NewID()
		c.PasswordEnc = ""
	}
	if err := c.Validate(); err != nil {
		fail(w, 400, err)
		return
	}
	if in.Password != "" {
		if strings.ContainsAny(in.Password, "\r\n") {
			fail(w, 400, errors.New(T("the password cannot contain line breaks")))
			return
		}
		enc, err := d.box.Encrypt(in.Password, c.ID)
		if err != nil {
			fail(w, 500, err)
			return
		}
		c.PasswordEnc = enc
	}
	if existing != nil {
		*existing = c
	} else {
		d.cfg.Connections = append(d.cfg.Connections, c)
	}
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	slog.Info("connection saved", "name", c.Name, "host", c.Host, "user", c.Username)
	go d.fwRefresh(true) // the outgoing SMB rule follows the connection hosts
	reply(w, viewConn(c))
}

func (d *Daemon) handleDeleteConn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, j := range d.cfg.Jobs {
		if j.Source.ConnectionID == id || j.Dest.ConnectionID == id {
			fail(w, 409, errors.New(Tf("connection used by the job %q", j.Name)))
			return
		}
	}
	prev := cloneConfig(d.cfg)
	out := d.cfg.Connections[:0:0]
	for _, c := range d.cfg.Connections {
		if c.ID != id {
			out = append(out, c)
		}
	}
	d.cfg.Connections = out
	if err := d.saveConfigLocked(prev); err != nil {
		fail(w, 500, err)
		return
	}
	go d.fwRefresh(true)
	reply(w, map[string]bool{"ok": true})
}

func (d *Daemon) handleTestConn(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	cfg := cloneConfig(d.cfg)
	d.mu.Unlock()
	id := r.PathValue("id")
	if cfg.Connection(id) == nil {
		fail(w, 404, errors.New(T("connection not found")))
		return
	}
	t, err := d.smbTarget(cfg, config.Location{Type: config.LocSMB, ConnectionID: id})
	if err != nil {
		reply(w, api.TestResult{OK: false, Message: err.Error()})
		return
	}
	shares, err := mount.ListShares(r.Context(), t)
	if err != nil {
		reply(w, api.TestResult{OK: false, Message: err.Error()})
		return
	}
	reply(w, api.TestResult{OK: true, Shares: shares,
		Message: Tf("access granted: %d shares found", len(shares))})
}

// --- browse ---

// openLocation makes a location accessible for browsing: local folders directly,
// SMB shares through a temporary mount (read-only unless writable is true).
// It returns the folder, the normalised location and a cleanup function.
func (d *Daemon) openLocation(ctx context.Context, loc config.Location, writable bool) (string, config.Location, func(), error) {
	switch loc.Type {
	case config.LocLocal:
		if loc.Path == "" {
			loc.Path = "/"
		}
		dir := filepath.Clean(loc.Path)
		if !filepath.IsAbs(dir) {
			return "", loc, nil, errors.New(T("path is not absolute"))
		}
		loc.Path = dir
		return dir, loc, func() {}, nil
	case config.LocSMB:
		p, err := config.CleanSubPath(loc.Path)
		if err != nil {
			return "", loc, nil, err
		}
		loc.Path = p
		d.mu.Lock()
		cfg := cloneConfig(d.cfg)
		d.mu.Unlock()
		t, err := d.smbTarget(cfg, loc)
		if err != nil {
			return "", loc, nil, err
		}
		m, err := mount.MountSMB(ctx, "browse-"+config.NewID(), t, !writable)
		if err != nil {
			return "", loc, nil, err
		}
		return filepath.Join(m.Point, loc.Path), loc, func() { m.Unmount() }, nil
	}
	return "", loc, nil, errors.New(T("invalid type"))
}

func (d *Daemon) handleBrowse(w http.ResponseWriter, r *http.Request) {
	var in api.BrowseRequest
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	dir, loc, cleanup, err := d.openLocation(ctx, in.Location, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer cleanup()
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail(w, 400, fmt.Errorf(T("reading folder: %w"), err))
		return
	}
	resp := api.BrowseResponse{Path: loc.Path, Dirs: []string{}}
	for _, e := range entries {
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if isDir && e.Name() != config.ArchiveDirName {
			resp.Dirs = append(resp.Dirs, e.Name())
		}
	}
	sort.Slice(resp.Dirs, func(a, b int) bool { return strings.ToLower(resp.Dirs[a]) < strings.ToLower(resp.Dirs[b]) })
	reply(w, resp)
}

// handleMkdir creates a new folder inside a location (used when choosing a destination).
func (d *Daemon) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var in api.MkdirRequest
	if err := decode(r, &in); err != nil {
		fail(w, 400, err)
		return
	}
	name, err := config.CleanFolderName(in.Name)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	dir, loc, cleanup, err := d.openLocation(ctx, in.Location, true)
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer cleanup()
	if err := os.Mkdir(filepath.Join(dir, name), 0o775); err != nil {
		if errors.Is(err, os.ErrExist) {
			fail(w, 409, errors.New(T("a folder with this name already exists")))
			return
		}
		fail(w, 400, fmt.Errorf(T("creating the folder: %w"), err))
		return
	}
	newPath := path.Join(loc.Path, name)
	if loc.Type == config.LocSMB {
		newPath = strings.TrimPrefix(newPath, "/")
	}
	slog.Info("folder created", "location", loc.Display(d.cfg), "name", name)
	reply(w, map[string]string{"path": newPath})
}

// --- history and logs ---

func (d *Daemon) handleHistory(w http.ResponseWriter, r *http.Request) {
	job := r.URL.Query().Get("job")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []api.Run{}
	for i := len(d.history) - 1; i >= 0 && len(out) < limit; i-- {
		if job == "" || d.history[i].JobID == job {
			out = append(out, d.history[i])
		}
	}
	reply(w, out)
}

func (d *Daemon) handleLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		fail(w, 400, errors.New(T("invalid id")))
		return
	}
	max, _ := strconv.Atoi(r.URL.Query().Get("max"))
	if max <= 0 || max > 4<<20 {
		max = 512 << 10
	}
	f, err := os.Open(logPath(id))
	if err != nil {
		fail(w, 404, errors.New(T("log not available")))
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	prefix := ""
	if st != nil && st.Size() > int64(max) {
		f.Seek(st.Size()-int64(max), io.SeekStart)
		prefix = T("… (log truncated, only the last lines are shown)") + "\n"
	}
	b, _ := io.ReadAll(f)
	reply(w, map[string]string{"log": prefix + string(b)})
}
