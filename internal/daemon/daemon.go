// Package daemon contains the service: scheduler, job execution and API.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/secrets"
)

const (
	historyPerJob = 100
	tickInterval  = 2 * time.Second
)

type running struct {
	run    *api.Run
	cancel context.CancelFunc
}

type Daemon struct {
	Version string

	mu      sync.Mutex
	cfg     *config.Config
	box     *secrets.Box
	running map[string]*running
	history []api.Run // chronological order
	next    map[string]time.Time
	sem     chan struct{}
	started time.Time
	wg      sync.WaitGroup
}

func New(version string) (*Daemon, error) {
	box, err := secrets.LoadOrCreate(paths.KeyFile())
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	setLanguage(cfg.Language)
	d := &Daemon{
		Version: version,
		cfg:     cfg,
		box:     box,
		running: map[string]*running{},
		next:    map[string]time.Time{},
		sem:     make(chan struct{}, cfg.MaxParallel),
		started: time.Now(),
	}
	for _, w := range d.warnings() {
		slog.Warn(w)
	}
	d.loadHistory()
	return d, nil
}

// setLanguage applies the configured language; VEGASYNCOR_LANG overrides it.
func setLanguage(configured string) {
	if env := i18n.FromEnv(); env != "" {
		configured = env
	}
	i18n.SetLang(configured)
}

// warnings lists the problems of the environment (container, missing commands),
// computed on every call so they follow the current language.
func (d *Daemon) warnings() []string {
	var out []string
	if !paths.DevMode() {
		if p := mount.DetectEnvironment().MountProblem(); p != "" {
			out = append(out, p)
		}
	}
	if err := mount.EnsureTools(); err != nil {
		out = append(out, err.Error())
	}
	return out
}

// Run starts the scheduler and the API until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if stale := mount.CleanupStale(); len(stale) > 0 {
		slog.Warn("unmounted leftover mount points", "points", stale)
	}
	srv, err := d.serveAPI()
	if err != nil {
		return err
	}
	slog.Info("VegaSyncor started", "version", d.Version, "socket", paths.Socket(), "jobs", len(d.cfg.Jobs),
		"language", i18n.Lang())

	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down: stopping the running jobs")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
			d.mu.Lock()
			for _, r := range d.running {
				r.cancel()
			}
			d.mu.Unlock()
			d.wg.Wait()
			os.Remove(paths.Socket())
			return nil
		case now := <-t.C:
			d.tick(now)
		}
	}
}

func (d *Daemon) tick(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, j := range d.cfg.Jobs {
		if !j.Enabled || j.Schedule.Type == config.SchedManual {
			delete(d.next, j.ID)
			continue
		}
		n, ok := d.next[j.ID]
		if !ok || n.IsZero() {
			d.next[j.ID] = j.Schedule.Next(now)
			continue
		}
		if now.Before(n) {
			continue
		}
		d.next[j.ID] = j.Schedule.Next(now)
		if _, busy := d.running[j.ID]; busy {
			slog.Warn("run skipped: the previous run is still in progress", "job", j.Name)
			d.addHistory(api.Run{
				ID: runID(j.ID, now), JobID: j.ID, JobName: j.Name, Trigger: api.TriggerScheduled,
				Start: now, End: now, Status: api.StatusSkipped,
				Message: T("skipped: the previous run was still in progress"),
			})
			continue
		}
		d.startLocked(j, false, api.TriggerScheduled)
	}
}

func runID(jobID string, t time.Time) string {
	return t.Format("20060102-150405") + "-" + jobID
}

// startLocked starts a job in the background. Requires d.mu.
func (d *Daemon) startLocked(j config.Job, dry bool, trigger string) (*api.Run, error) {
	if _, busy := d.running[j.ID]; busy {
		return nil, errors.New(T("the job is already running"))
	}
	now := time.Now()
	r := &api.Run{
		ID: runID(j.ID, now), JobID: j.ID, JobName: j.Name, Trigger: trigger, DryRun: dry,
		Start: now, Status: api.StatusRunning, Phase: T("queued"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.running[j.ID] = &running{run: r, cancel: cancel}
	// recorded right away: if the service stops abruptly (reboot, power loss)
	// it shows up as "interrupted" after the restart instead of disappearing
	d.addHistory(api.Run{ID: r.ID, JobID: r.JobID, JobName: r.JobName, Trigger: r.Trigger,
		DryRun: r.DryRun, Start: r.Start, Status: api.StatusRunning})
	cfgSnap := *d.cfg
	cfgSnap.Connections = append([]config.Connection(nil), d.cfg.Connections...)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer cancel()
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			d.finish(j.ID, r, api.StatusCancelled, T("cancelled before starting"))
			return
		}
		defer func() { <-d.sem }()
		d.execute(ctx, &cfgSnap, j, r)
	}()
	return r, nil
}

func (d *Daemon) setPhase(r *api.Run, phase string) {
	d.mu.Lock()
	r.Phase = phase
	d.mu.Unlock()
}

func (d *Daemon) finish(jobID string, r *api.Run, status, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r.End = time.Now()
	r.Status = status
	r.Message = strings.Join(strings.Fields(msg), " ") // always on one line
	r.Phase = ""
	r.Progress = nil
	delete(d.running, jobID)
	d.addHistory(*r)
	lvl := slog.LevelInfo
	if status == api.StatusError {
		lvl = slog.LevelError
	} else if status == api.StatusWarning {
		lvl = slog.LevelWarn
	}
	slog.Log(context.Background(), lvl, "job finished", "job", r.JobName, "status", status,
		"message", msg, "files", r.Stats.FilesTransferred, "bytes", r.Stats.BytesTransferred,
		"duration", r.Duration().Round(time.Second).String(), "dry_run", r.DryRun)
}

// --- history ---

func (d *Daemon) loadHistory() {
	raw, err := os.ReadFile(paths.HistoryFile())
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, &d.history); err != nil {
		slog.Warn("unreadable history, it will be recreated", "error", err)
		d.history = nil
		return
	}
	interrupted := false
	for i := range d.history {
		h := &d.history[i]
		if h.Status != api.StatusRunning {
			continue
		}
		// run in progress when the service stopped abruptly
		h.Status = api.StatusError
		h.Message = T("interrupted: the service stopped during the copy (reboot or shutdown)")
		h.Progress = nil
		h.End = h.Start
		if st, err := os.Stat(logPath(h.ID)); err == nil && st.ModTime().After(h.Start) {
			h.End = st.ModTime() // last write to the log ≈ time of the interruption
		}
		interrupted = true
	}
	if interrupted {
		if err := d.saveHistory(); err != nil {
			slog.Error("saving history", "error", err)
		}
	}
}

// addHistory adds (or updates) a history entry and saves it. Requires d.mu.
func (d *Daemon) addHistory(r api.Run) {
	replaced := false
	for i := len(d.history) - 1; i >= 0; i-- {
		if d.history[i].ID == r.ID {
			d.history[i] = r // updates the entry recorded at start
			replaced = true
			break
		}
	}
	if !replaced {
		d.history = append(d.history, r)
	}
	// limit the number of entries per job and delete the matching logs
	var drop []api.Run
	d.history, drop = trimHistory(d.history, historyPerJob)
	for _, h := range drop {
		os.Remove(logPath(h.ID))
	}
	if err := d.saveHistory(); err != nil {
		slog.Error("saving history", "error", err)
	}
}

// trimHistory keeps at most perJob entries (the most recent) for each job,
// preserving the chronological order. It also returns the discarded entries.
func trimHistory(h []api.Run, perJob int) (keep, drop []api.Run) {
	count := map[string]int{}
	skip := make([]bool, len(h))
	for i := len(h) - 1; i >= 0; i-- {
		count[h[i].JobID]++
		skip[i] = count[h[i].JobID] > perJob
	}
	keep = make([]api.Run, 0, len(h))
	for i, r := range h {
		if skip[i] {
			drop = append(drop, r)
		} else {
			keep = append(keep, r)
		}
	}
	return keep, drop
}

func (d *Daemon) saveHistory() error {
	raw, err := json.Marshal(d.history)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StateDir(), 0o750); err != nil {
		return err
	}
	tmp := paths.HistoryFile() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, paths.HistoryFile())
}

func logPath(runID string) string { return filepath.Join(paths.LogDir(), runID+".log") }

func (d *Daemon) lastRun(jobID string) *api.Run {
	for i := len(d.history) - 1; i >= 0; i-- {
		if d.history[i].JobID == jobID && d.history[i].Status != api.StatusRunning {
			r := d.history[i]
			return &r
		}
	}
	return nil
}

// --- status ---

func (d *Daemon) status() api.Status {
	warnings := d.warnings() // outside the lock: reads system files
	d.mu.Lock()
	defer d.mu.Unlock()
	host, _ := os.Hostname()
	now := time.Now()
	abbr, _ := now.Zone()
	s := api.Status{
		Version: d.Version, Started: d.started, Hostname: host,
		MaxParallel: d.cfg.MaxParallel, Warnings: warnings,
		ServerTime: now, ZoneAbbr: abbr, ZoneName: zoneName(), Language: i18n.Lang(),
	}
	for _, j := range d.cfg.Jobs {
		js := api.JobStatus{
			Job: j, Source: j.Source.Display(d.cfg), Dest: j.Dest.Display(d.cfg),
			Last: d.lastRun(j.ID), Next: d.next[j.ID],
		}
		if r, ok := d.running[j.ID]; ok {
			cp := *r.run
			if r.run.Progress != nil {
				p := *r.run.Progress
				cp.Progress = &p
			}
			js.Current = &cp
		}
		s.Jobs = append(s.Jobs, js)
	}
	sort.SliceStable(s.Jobs, func(a, b int) bool { return s.Jobs[a].Job.Name < s.Jobs[b].Job.Name })
	for _, c := range d.cfg.Connections {
		s.Connections = append(s.Connections, viewConn(c))
	}
	return s
}

func viewConn(c config.Connection) api.ConnectionView {
	v := api.ConnectionView{Connection: c, HasPassword: c.PasswordEnc != ""}
	v.PasswordEnc = ""
	return v
}

func (d *Daemon) smbTarget(cfg *config.Config, loc config.Location) (mount.SMBTarget, error) {
	cn := cfg.Connection(loc.ConnectionID)
	if cn == nil {
		return mount.SMBTarget{}, errors.New(Tf("connection %q not found", loc.ConnectionID))
	}
	pw, err := d.box.Decrypt(cn.PasswordEnc, cn.ID)
	if err != nil {
		return mount.SMBTarget{}, fmt.Errorf("%s %s: %w", T("connection"), cn.Name, err)
	}
	return mount.SMBTarget{
		Host: cn.Host, Share: loc.Share, Username: cn.Username, Password: pw,
		Domain: cn.Domain, SMBVersion: cn.SMBVersion,
	}, nil
}

// zoneName returns the name of the system time zone (e.g. Europe/Rome), if known.
func zoneName() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		return tz
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.Index(p, "zoneinfo/"); i >= 0 {
			return p[i+len("zoneinfo/"):]
		}
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(raw))
	}
	return ""
}
