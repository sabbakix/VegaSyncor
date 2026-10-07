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

// prepareFolder makes a folder the job writes to (archive, logs) accessible,
// creating it when create is set, and returns the actual directory.
func (d *Daemon) prepareFolder(ctx context.Context, cfg *config.Config, name string, loc config.Location, create bool) (string, *mount.Mount, error) {
	switch loc.Type {
	case config.LocSMB:
		t, err := d.smbTarget(cfg, loc)
		if err != nil {
			return "", nil, err
		}
		m, err := mount.MountSMB(ctx, name, t, false)
		if err != nil {
			return "", nil, err
		}
		dir := filepath.Join(m.Point, loc.Path)
		if create {
			if err := os.MkdirAll(dir, 0o770); err != nil {
				m.Unmount()
				return "", nil, err
			}
		}
		return dir, m, nil
	case config.LocLocal:
		if create {
			if err := os.MkdirAll(loc.Path, 0o750); err != nil {
				return "", nil, err
			}
		}
		return loc.Path, nil, nil
	}
	return "", nil, errors.New(T("unknown location type"))
}

func (d *Daemon) execute(ctx context.Context, cfg *config.Config, j config.Job, r *api.Run) {
	status, msg := d.doExecute(ctx, cfg, j, r)
	if j.LogDir != nil {
		if err := d.saveLogCopy(cfg, j, r); err != nil {
			appendLog(r.ID, Tf("WARNING: log copy not saved in %s: %v", j.LogDir.Display(cfg), err))
			if status == api.StatusOK {
				status, msg = api.StatusWarning, msg+" – "+T("log copy not saved")
			}
		}
	}
	d.finish(j.ID, r, status, msg)
}

// saveLogCopy copies the run log into the log folder of the job and compresses
// the old logs there.
func (d *Daemon) saveLogCopy(cfg *config.Config, j config.Job, r *api.Run) error {
	d.setPhase(r, T("saving the log"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir, m, err := d.prepareFolder(ctx, cfg, j.ID+"-log", *j.LogDir, true)
	if err != nil {
		return err
	}
	defer m.Unmount()
	if _, err := syncer.SaveLog(dir, j.Name, r.Start.Format(syncer.StampFormat), r.DryRun, logPath(r.ID)); err != nil {
		return err
	}
	if n, err := syncer.CompressLogs(dir, j.Name, j.LogCompressDays, time.Now()); err != nil {
		appendLog(r.ID, Tf("warning: compressing the old logs: %v", err))
	} else if n > 0 {
		appendLog(r.ID, Tf("logs: %d logs older than %d days moved into the monthly zip files", n, j.LogCompressDays))
	}
	return nil
}

// appendLog adds a line to the log of a run after the sync has finished.
func appendLog(runID, text string) {
	f, err := os.OpenFile(logPath(runID), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), text)
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
	if j.Mode == config.ModeMirrorArchive && j.Archive != nil {
		logf(Tf("Deleted to:  %s", j.Archive.Display(cfg)))
	}
	if j.LogDir != nil {
		logf(Tf("Log copy:    %s", j.LogDir.Display(cfg)))
	}
	if j.Checksum {
		logf(T("Comparison:  file contents (checksum): every file is read on both sides"))
	}

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

	// folders of the job inside the destination: the sync must not touch them
	var protect []string
	for _, l := range []*config.Location{j.Archive, j.LogDir} {
		if l != nil {
			if rel, ok := cfg.RelPath(*l, j.Dest); ok && rel != "" {
				protect = append(protect, rel)
			}
		}
	}
	archiveDir := filepath.Join(dst, config.ArchiveDirName)
	if j.Mode == config.ModeMirrorArchive && j.Archive != nil {
		a := *j.Archive
		switch rel, inside := cfg.RelPath(a, j.Dest); {
		case inside:
			archiveDir = filepath.Join(dst, rel)
		case a.Type == config.LocSMB && dm != nil && a.ConnectionID == j.Dest.ConnectionID && strings.EqualFold(a.Share, j.Dest.Share):
			// same share as the destination: reuse its mount, so files are moved, not copied
			archiveDir = filepath.Join(dm.Point, a.Path)
		default:
			d.setPhase(r, T("connecting to the deleted items folder"))
			dir, am, err := d.prepareFolder(ctx, cfg, j.ID+"-arc", a, false)
			if err != nil {
				return fail(T("deleted items folder"), err)
			}
			defer func() {
				if err := am.Unmount(); err != nil {
					logf(Tf("warning: unmounting the deleted items folder: %v", err))
				}
			}()
			archiveDir = dir
		}
		if !r.DryRun {
			if err := os.MkdirAll(archiveDir, 0o770); err != nil {
				return fail(T("deleted items folder"), err)
			}
		}
	}

	d.setPhase(r, T("synchronizing"))
	opts := syncer.Options{
		Src: src, Dst: dst, Mode: j.Mode, Excludes: j.Excludes, DryRun: r.DryRun,
		BandwidthKBps: j.BandwidthKBps, Checksum: j.Checksum, RunStamp: r.Start.Format(syncer.StampFormat),
		ArchiveDir: archiveDir, Protect: protect,
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
		removed, err := syncer.PruneArchive(archiveDir, j.ArchiveDays, time.Now())
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
