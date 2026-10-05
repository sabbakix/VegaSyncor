package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/mount"
	"vegasyncor/internal/paths"
	"vegasyncor/internal/syncer"
)

// prepare makes a Location accessible and returns the actual directory.
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
			return "", nil, fmt.Errorf(T("folder %s not accessible: %w"), loc.Path, err)
		}
		if !st.IsDir() {
			return "", nil, errors.New(Tf("%s is not a folder", loc.Path))
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
	return "", nil, errors.New(T("unknown location type"))
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
	logf := func(text string) {
		fmt.Fprintf(lf, "[%s] %s\n", time.Now().Format("15:04:05"), text)
	}
	fail := func(phase string, err error) (string, string) {
		if ctx.Err() != nil {
			logf(Tf("cancelled during: %s", phase))
			return api.StatusCancelled, T("cancelled")
		}
		logf(Tf("ERROR (%s): %v", phase, err))
		return api.StatusError, fmt.Sprintf("%s: %v", phase, err)
	}

	kind := T("real run")
	if r.DryRun {
		kind = T("DRY RUN")
	}
	ro := ""
	if j.SourceRO {
		ro = " [" + T("read-only") + "]"
	}
	logf(fmt.Sprintf("Job %q (%s) – %s", j.Name, config.ModeLabel(j.Mode), kind))
	logf(Tf("Source:      %s", j.Source.Display(cfg)) + ro)
	logf(Tf("Destination: %s", j.Dest.Display(cfg)))

	d.setPhase(r, T("connecting to source"))
	src, sm, err := d.prepare(ctx, cfg, j.ID+"-src", j.Source, j.SourceRO)
	if err != nil {
		return fail(T("source"), err)
	}
	defer func() {
		if err := sm.Unmount(); err != nil {
			logf(Tf("warning: unmounting source: %v", err))
		}
	}()
	st, err := os.Stat(src)
	if err != nil || !st.IsDir() {
		return fail(T("source"), errors.New(Tf("the folder %s does not exist in the share", j.Source.Path)))
	}
	if j.Mode != config.ModeAdditive && !j.AllowEmptySource {
		if empty, err := syncer.IsEmptyDir(src); err != nil {
			return fail(T("source"), err)
		} else if empty {
			return fail(T("safety check"), errors.New(T("the source is empty: mirror blocked so the destination is not wiped (enable 'Allow empty source' if intended)")))
		}
	}

	d.setPhase(r, T("connecting to destination"))
	dst, dm, err := d.prepare(ctx, cfg, j.ID+"-dst", j.Dest, false)
	if err != nil {
		return fail(T("destination"), err)
	}
	defer func() {
		if err := dm.Unmount(); err != nil {
			logf(Tf("warning: unmounting destination: %v", err))
		}
	}()
	if j.Dest.Type == config.LocSMB && j.Dest.Path != "" && !r.DryRun {
		if err := os.MkdirAll(dst, 0o770); err != nil {
			return fail(T("destination"), err)
		}
	}

	d.setPhase(r, T("synchronizing"))
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
		logf(T("cancelled by the user"))
		return api.StatusCancelled, T("cancelled")
	}
	if err != nil {
		return fail("rsync", err)
	}

	if j.Mode == config.ModeMirrorArchive && !r.DryRun && j.ArchiveDays > 0 {
		d.setPhase(r, T("cleaning up the archive"))
		removed, err := syncer.PruneArchive(dst, j.ArchiveDays, time.Now())
		if err != nil {
			logf(Tf("warning: archive cleanup: %v", err))
		} else if len(removed) > 0 {
			logf(Tf("archive: removed %d folders older than %d days", len(removed), j.ArchiveDays))
		}
	}

	size := api.HumanBytes(res.Stats.BytesTransferred)
	var summary string
	switch {
	case r.DryRun && j.Mode != config.ModeAdditive:
		summary = Tf("dry run: %d files to copy (%s), %d to delete", res.Stats.FilesTransferred, size, res.Stats.FilesDeleted)
	case r.DryRun:
		summary = Tf("dry run: %d files to copy (%s)", res.Stats.FilesTransferred, size)
	case j.Mode != config.ModeAdditive:
		summary = Tf("%d files copied (%s), %d deleted", res.Stats.FilesTransferred, size, res.Stats.FilesDeleted)
	default:
		summary = Tf("%d files copied (%s)", res.Stats.FilesTransferred, size)
	}
	logf(Tf("Finished: %s", summary))
	if res.Warning != "" {
		logf(Tf("WARNING: %s", res.Warning))
		return api.StatusWarning, summary + " – " + res.Warning
	}
	return api.StatusOK, summary
}
