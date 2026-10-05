// Package daemon contiene il servizio: pianificatore, esecuzione dei job e API.
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

	mu       sync.Mutex
	cfg      *config.Config
	box      *secrets.Box
	running  map[string]*running
	history  []api.Run // ordine cronologico
	next     map[string]time.Time
	sem      chan struct{}
	started  time.Time
	warnings []string
	wg       sync.WaitGroup
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
	d := &Daemon{
		Version: version,
		cfg:     cfg,
		box:     box,
		running: map[string]*running{},
		next:    map[string]time.Time{},
		sem:     make(chan struct{}, cfg.MaxParallel),
		started: time.Now(),
	}
	if !paths.DevMode() {
		if p := mount.DetectEnvironment().MountProblem(); p != "" {
			d.warnings = append(d.warnings, p)
		}
	}
	if err := mount.EnsureTools(); err != nil {
		d.warnings = append(d.warnings, err.Error())
	}
	for _, w := range d.warnings {
		slog.Warn(w)
	}
	d.loadHistory()
	return d, nil
}

// Run avvia pianificatore e API finché ctx non viene annullato.
func (d *Daemon) Run(ctx context.Context) error {
	if stale := mount.CleanupStale(); len(stale) > 0 {
		slog.Warn("smontati punti di montaggio residui", "punti", stale)
	}
	srv, err := d.serveAPI()
	if err != nil {
		return err
	}
	slog.Info("VegaSyncor avviato", "versione", d.Version, "socket", paths.Socket(), "job", len(d.cfg.Jobs))

	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("arresto in corso: interruzione dei job attivi")
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
			slog.Warn("esecuzione saltata: il job precedente è ancora in corso", "job", j.Name)
			d.addHistory(api.Run{
				ID: runID(j.ID, now), JobID: j.ID, JobName: j.Name, Trigger: "pianificato",
				Start: now, End: now, Status: api.StatusSkipped,
				Message: "saltato: l'esecuzione precedente era ancora in corso",
			})
			continue
		}
		d.startLocked(j, false, "pianificato")
	}
}

func runID(jobID string, t time.Time) string {
	return t.Format("20060102-150405") + "-" + jobID
}

// startLocked avvia un job in background. Richiede d.mu.
func (d *Daemon) startLocked(j config.Job, dry bool, trigger string) (*api.Run, error) {
	if _, busy := d.running[j.ID]; busy {
		return nil, errors.New("il job è già in esecuzione")
	}
	now := time.Now()
	r := &api.Run{
		ID: runID(j.ID, now), JobID: j.ID, JobName: j.Name, Trigger: trigger, DryRun: dry,
		Start: now, Status: api.StatusRunning, Phase: "in coda",
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.running[j.ID] = &running{run: r, cancel: cancel}
	// registrata subito: se il servizio si interrompe di colpo (riavvio, mancanza
	// di corrente) al riavvio risulterà "interrotta" invece di sparire
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
			d.finish(j.ID, r, api.StatusCancelled, "annullato prima dell'avvio")
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
	r.Message = strings.Join(strings.Fields(msg), " ") // sempre su una riga
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
	slog.Log(context.Background(), lvl, "job concluso", "job", r.JobName, "esito", status,
		"messaggio", msg, "file", r.Stats.FilesTransferred, "byte", r.Stats.BytesTransferred,
		"durata", r.Duration().Round(time.Second).String(), "simulazione", r.DryRun)
}

// --- storico ---

func (d *Daemon) loadHistory() {
	raw, err := os.ReadFile(paths.HistoryFile())
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, &d.history); err != nil {
		slog.Warn("storico illeggibile, verrà ricreato", "errore", err)
		d.history = nil
		return
	}
	interrupted := false
	for i := range d.history {
		h := &d.history[i]
		if h.Status != api.StatusRunning {
			continue
		}
		// esecuzione in corso quando il servizio si è fermato di colpo
		h.Status = api.StatusError
		h.Message = "interrotta: il servizio si è arrestato durante la copia (riavvio o spegnimento)"
		h.Progress = nil
		h.End = h.Start
		if st, err := os.Stat(logPath(h.ID)); err == nil && st.ModTime().After(h.Start) {
			h.End = st.ModTime() // ultima scrittura del log ≈ momento dell'interruzione
		}
		interrupted = true
	}
	if interrupted {
		if err := d.saveHistory(); err != nil {
			slog.Error("salvataggio storico", "errore", err)
		}
	}
}

// addHistory aggiunge una voce allo storico e lo salva. Richiede d.mu.
func (d *Daemon) addHistory(r api.Run) {
	replaced := false
	for i := len(d.history) - 1; i >= 0; i-- {
		if d.history[i].ID == r.ID {
			d.history[i] = r // aggiorna la voce registrata all'avvio
			replaced = true
			break
		}
	}
	if !replaced {
		d.history = append(d.history, r)
	}
	// limita il numero di voci per job ed elimina i log corrispondenti
	var drop []api.Run
	d.history, drop = trimHistory(d.history, historyPerJob)
	for _, h := range drop {
		os.Remove(logPath(h.ID))
	}
	if err := d.saveHistory(); err != nil {
		slog.Error("salvataggio storico", "errore", err)
	}
}

// trimHistory mantiene al massimo perJob voci (le più recenti) per ciascun job,
// preservando l'ordine cronologico. Restituisce anche le voci scartate.
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

// --- stato ---

func (d *Daemon) status() api.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	host, _ := os.Hostname()
	now := time.Now()
	abbr, _ := now.Zone()
	s := api.Status{
		Version: d.Version, Started: d.started, Hostname: host,
		MaxParallel: d.cfg.MaxParallel, Warnings: d.warnings,
		ServerTime: now, ZoneAbbr: abbr, ZoneName: zoneName(),
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
		return mount.SMBTarget{}, fmt.Errorf("connessione %q non trovata", loc.ConnectionID)
	}
	pw, err := d.box.Decrypt(cn.PasswordEnc, cn.ID)
	if err != nil {
		return mount.SMBTarget{}, fmt.Errorf("connessione %s: %w", cn.Name, err)
	}
	return mount.SMBTarget{
		Host: cn.Host, Share: loc.Share, Username: cn.Username, Password: pw,
		Domain: cn.Domain, SMBVersion: cn.SMBVersion,
	}, nil
}

// zoneName restituisce il nome del fuso orario del sistema (es. Europe/Rome), se determinabile.
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
