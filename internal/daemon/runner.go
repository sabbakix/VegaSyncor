package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/syncer"
)

// prepare rende accessibile una Location e restituisce la directory effettiva.
func (d *Daemon) prepare(ctx context.Context, cfg *config.Config, name string, loc config.Location, readOnly bool) (string, *mount.Mount, error) {
	switch loc.Type {
	case config.LocSMB:
		t, err := d.smbTarget(cfg, loc)
		if err != nil {
			return "", nil, err
		}
		m, err := mount.MountSMB(ctx, name, t, readOnly)
		if err != nil {
			return "", nil, err
		}
		return filepath.Join(m.Point, loc.Path), m, nil
	case config.LocLocal:
		st, err := os.Stat(loc.Path)
		if err != nil {
			return "", nil, fmt.Errorf("cartella %s non accessibile: %w", loc.Path, err)
		}
		if !st.IsDir() {
			return "", nil, fmt.Errorf("%s non è una cartella", loc.Path)
		}
		if !readOnly {
			return loc.Path, nil, nil
		}
		m, err := mount.BindReadOnly(ctx, name, loc.Path)
		if err != nil {
			return "", nil, err
		}
		return m.Point, m, nil
	}
	return "", nil, errors.New("tipo di posizione sconosciuto")
}

func (d *Daemon) execute(ctx context.Context, cfg *config.Config, j config.Job, r *api.Run) {
	status, msg := d.doExecute(ctx, cfg, j, r)
	d.finish(j.ID, r, status, msg)
}

func (d *Daemon) doExecute(ctx context.Context, cfg *config.Config, j config.Job, r *api.Run) (string, string) {
	if err := os.MkdirAll(paths.LogDir(), 0o750); err != nil {
		return api.StatusError, err.Error()
	}
	lf, err := os.OpenFile(logPath(r.ID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return api.StatusError, err.Error()
	}
	defer lf.Close()
	logf := func(format string, a ...any) {
		fmt.Fprintf(lf, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
	}
	fail := func(phase string, err error) (string, string) {
		if ctx.Err() != nil {
			logf("annullato durante: %s", phase)
			return api.StatusCancelled, "annullato"
		}
		logf("ERRORE (%s): %v", phase, err)
		return api.StatusError, fmt.Sprintf("%s: %v", phase, err)
	}

	logf("Job %q (%s) – %s", j.Name, config.ModeLabel(j.Mode), map[bool]string{true: "SIMULAZIONE", false: "esecuzione reale"}[r.DryRun])
	logf("Sorgente:     %s %s", j.Source.Display(cfg), map[bool]string{true: "[sola lettura]", false: ""}[j.SourceRO])
	logf("Destinazione: %s", j.Dest.Display(cfg))

	d.setPhase(r, "connessione sorgente")
	src, sm, err := d.prepare(ctx, cfg, j.ID+"-src", j.Source, j.SourceRO)
	if err != nil {
		return fail("sorgente", err)
	}
	defer func() {
		if err := sm.Unmount(); err != nil {
			logf("attenzione: smontaggio sorgente: %v", err)
		}
	}()
	st, err := os.Stat(src)
	if err != nil || !st.IsDir() {
		return fail("sorgente", fmt.Errorf("la cartella %s non esiste nella condivisione", j.Source.Path))
	}
	if j.Mode != config.ModeAdditive && !j.AllowEmptySource {
		if empty, err := syncer.IsEmptyDir(src); err != nil {
			return fail("sorgente", err)
		} else if empty {
			return fail("controllo sicurezza", errors.New("la sorgente è vuota: mirror bloccato per non svuotare la destinazione (abilitare 'Consenti sorgente vuota' se voluto)"))
		}
	}

	d.setPhase(r, "connessione destinazione")
	dst, dm, err := d.prepare(ctx, cfg, j.ID+"-dst", j.Dest, false)
	if err != nil {
		return fail("destinazione", err)
	}
	defer func() {
		if err := dm.Unmount(); err != nil {
			logf("attenzione: smontaggio destinazione: %v", err)
		}
	}()
	if j.Dest.Type == config.LocSMB && j.Dest.Path != "" && !r.DryRun {
		if err := os.MkdirAll(dst, 0o770); err != nil {
			return fail("destinazione", err)
		}
	}

	d.setPhase(r, "sincronizzazione")
	opts := syncer.Options{
		Src: src, Dst: dst, Mode: j.Mode, Excludes: j.Excludes, DryRun: r.DryRun,
		BandwidthKBps: j.BandwidthKBps, RunStamp: r.Start.Format(syncer.StampFormat),
	}
	res, err := syncer.Run(ctx, opts, lf, func(p syncer.Progress) {
		d.mu.Lock()
		r.Progress = &p
		d.mu.Unlock()
	})
	d.mu.Lock()
	r.Stats = res.Stats
	d.mu.Unlock()
	if errors.Is(err, context.Canceled) {
		logf("annullato dall'utente")
		return api.StatusCancelled, "annullato"
	}
	if err != nil {
		return fail("rsync", err)
	}

	if j.Mode == config.ModeMirrorArchive && !r.DryRun && j.ArchiveDays > 0 {
		d.setPhase(r, "pulizia archivio")
		removed, err := syncer.PruneArchive(dst, j.ArchiveDays, time.Now())
		if err != nil {
			logf("attenzione: pulizia archivio: %v", err)
		} else if len(removed) > 0 {
			logf("archivio: rimosse %d cartelle più vecchie di %d giorni", len(removed), j.ArchiveDays)
		}
	}

	summary := fmt.Sprintf("%d file copiati (%s)", res.Stats.FilesTransferred, api.HumanBytes(res.Stats.BytesTransferred))
	if j.Mode != config.ModeAdditive {
		summary += fmt.Sprintf(", %d cancellati", res.Stats.FilesDeleted)
	}
	if r.DryRun {
		summary = "simulazione: " + strings.Replace(summary, "copiati", "da copiare", 1)
		summary = strings.Replace(summary, "cancellati", "da cancellare", 1)
	}
	logf("Concluso: %s", summary)
	if res.Warning != "" {
		logf("ATTENZIONE: %s", res.Warning)
		return api.StatusWarning, summary + " – " + res.Warning
	}
	return api.StatusOK, summary
}
