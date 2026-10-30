package cloud

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"vegasyncor/internal/config"
	"vegasyncor/internal/syncer"
)

const (
	archiveDirName = config.ArchiveDirName
	stampFormat    = syncer.StampFormat
)

// Options of a copy between a local folder and OneDrive (either direction).
type Options struct {
	Src           string // rclone path or local folder (already mounted)
	Dst           string // rclone path or local folder
	Mode          string
	Excludes      []string
	DryRun        bool
	BandwidthKBps int
	Checksum      bool
	// Archive is the rclone path of the archive folder (Mirror + archive); a
	// RunStamp subfolder is used for every run. "" = ArchiveDirName in Dst.
	Archive  string
	Protect  []string // folders inside the destination (relative) left alone
	RunStamp string
}

// event is a change reported by rclone, translated into the rsync itemized
// form used by the run logs (so the log viewer and its filters work the same).
type event struct {
	kind string // "new", "update", "delete"
	obj  string
	size int64
}

func (e event) line() string {
	switch e.kind {
	case "new":
		return ">f+++++++++ " + e.obj
	case "update":
		return ">f.st...... " + e.obj
	}
	return "*deleting   " + e.obj
}

type rcloneLog struct {
	Level   string     `json:"level"`
	Msg     string     `json:"msg"`
	Object  string     `json:"object"`
	Size    int64      `json:"size"`
	Skipped string     `json:"skipped"`
	Stats   *jsonStats `json:"stats"`
}

type jsonStats struct {
	Bytes        int64   `json:"bytes"`
	TotalBytes   int64   `json:"totalBytes"`
	Speed        float64 `json:"speed"`
	ETA          *int64  `json:"eta"`
	Errors       int64   `json:"errors"`
	FatalError   bool    `json:"fatalError"`
	Transferring []struct {
		Name string `json:"name"`
	} `json:"transferring"`
}

// Args builds the rclone command line for a copy from opts.Src to opts.Dst.
func Args(opts Options) []string {
	dest := opts.Dst
	cmd := "sync"
	if opts.Mode == config.ModeAdditive {
		cmd = "copy"
	}
	args := []string{cmd, opts.Src, dest,
		"--use-json-log", "-v", "--stats", "1s", "--stats-log-level", "INFO",
		"--modify-window", "2s", "--retries", "3"}
	excl := append(append([]string{}, syncer.DefaultExcludes...), opts.Excludes...)
	excl = append(excl, "/"+archiveDirName+"/**")
	for _, p := range opts.Protect {
		if p = strings.Trim(p, "/"); p != "" {
			excl = append(excl, "/"+escapeGlob(p)+"/**")
		}
	}
	for _, e := range excl {
		args = append(args, "--exclude", e)
	}
	if opts.Mode == config.ModeMirrorArchive {
		archive := opts.Archive
		if archive == "" {
			archive = Join(dest, archiveDirName)
		}
		args = append(args, "--backup-dir", Join(archive, opts.RunStamp))
	}
	if opts.Checksum {
		args = append(args, "--checksum")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	if opts.BandwidthKBps > 0 {
		args = append(args, fmt.Sprintf("--bwlimit=%dk", opts.BandwidthKBps))
	}
	return args
}

func escapeGlob(p string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `{`, `\{`).Replace(p)
}

// Sync copies opts.Src to opts.Dst (one of them is c.Root or inside it), writing
// the changes to log in the rsync itemized form and reporting progress.
func (c *Config) Sync(ctx context.Context, opts Options, log io.Writer, onProgress func(syncer.Progress)) (syncer.Result, error) {
	args := Args(opts)
	fmt.Fprintf(log, "# rclone %s\n", strings.Join(args, " "))
	cmd := c.command(ctx, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return syncer.Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return syncer.Result{}, err
	}

	var (
		res        syncer.Result
		events     []*event
		byObj      = map[string]*event{}
		backedUp   = map[string]bool{} // old version moved to the archive: a copy is an update
		dryCopies  []*event
		last       jsonStats
		lastNotify time.Time
		lastErr    string
		decryptErr bool // encrypted files that cannot be read: wrong password
	)
	add := func(kind, obj string, size int64) *event {
		e := &event{kind: kind, obj: obj, size: size}
		if prev := byObj[obj]; prev != nil {
			*prev = *e // a later event about the same file wins (e.g. moved → copied)
			return prev
		}
		byObj[obj] = e
		events = append(events, e)
		return e
	}
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		var l rcloneLog
		if json.Unmarshal([]byte(line), &l) != nil {
			if strings.TrimSpace(line) != "" {
				fmt.Fprintln(log, line)
				lastErr = stripStamp(line)
			}
			continue
		}
		if l.Stats != nil {
			last = *l.Stats
			if onProgress != nil && time.Since(lastNotify) > 300*time.Millisecond {
				lastNotify = time.Now()
				onProgress(progress(last, len(events)))
			}
			continue
		}
		obj := l.Object
		switch {
		case l.Msg == "Copied (new)":
			if backedUp[obj] {
				add("update", obj, 0)
			} else {
				add("new", obj, 0)
			}
		case l.Msg == "Copied (replaced existing)":
			add("update", obj, 0)
		case l.Msg == "Moved (server-side)" || l.Skipped == "move":
			backedUp[obj] = true // archived: followed by a copy (update) or "into backup dir" (delete)
			if l.Skipped == "move" {
				if e := byObj[obj]; e != nil && e.kind == "new" {
					e.kind = "update"
				}
			}
		case l.Msg == "Moved into backup dir" || l.Msg == "Deleted" || l.Skipped == "move into backup dir" || l.Skipped == "delete":
			add("delete", obj, l.Size)
		case l.Skipped == "copy":
			kind := "new"
			if backedUp[obj] {
				kind = "update"
			}
			e := add(kind, obj, l.Size)
			if kind == "new" {
				dryCopies = append(dryCopies, e)
			}
		case l.Skipped != "" || strings.HasPrefix(l.Msg, "Updated modification time") ||
			strings.HasPrefix(l.Msg, "Making directory") || strings.HasPrefix(l.Msg, "Removing directory") ||
			strings.HasPrefix(l.Msg, "Set directory modification time"):
			// folders and times only: not a change of the files
		case l.Level == "error" || l.Level == "critical":
			if c.encrypted && isDecryptError(l.Msg) {
				decryptErr = true
			}
			msg := l.Msg
			if obj != "" {
				msg = obj + ": " + msg
			}
			fmt.Fprintln(log, "ERROR: "+msg)
			lastErr = msg
		case l.Level == "warning":
			fmt.Fprintln(log, "warning: "+strings.TrimSpace(obj+" "+l.Msg))
			if c.encrypted && isDecryptError(l.Msg) {
				decryptErr = true
			}
		case l.Level == "notice":
			fmt.Fprintln(log, "[rclone] "+strings.TrimSpace(obj+" "+l.Msg))
		}
	}
	err = cmd.Wait()

	// dry run: a file that would be copied is an update if it exists already
	if opts.DryRun && len(dryCopies) > 0 && ctx.Err() == nil {
		existing, lerr := c.listFiles(ctx, opts)
		if lerr != nil {
			fmt.Fprintf(log, "warning: cannot tell new from changed files: %v\n", lerr)
		}
		for _, e := range dryCopies {
			if existing[e.obj] && e.kind == "new" {
				e.kind = "update"
			}
		}
	}
	for _, e := range events {
		fmt.Fprintln(log, e.line())
		switch e.kind {
		case "new", "update":
			res.Stats.FilesTransferred++
			if opts.DryRun {
				res.Stats.BytesTransferred += e.size
			}
		case "delete":
			res.Stats.FilesDeleted++
		}
	}
	if !opts.DryRun {
		res.Stats.BytesTransferred = last.Bytes
	}
	res.Stats.BytesTotal = last.TotalBytes
	fmt.Fprintf(log, "Number of regular files transferred: %d\nNumber of deleted files: %d\nTotal transferred file size: %d bytes\n",
		res.Stats.FilesTransferred, res.Stats.FilesDeleted, res.Stats.BytesTransferred)

	if ctx.Err() != nil {
		return res, context.Canceled
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		if !last.FatalError && last.Errors > 0 && (res.ExitCode == 2 || res.ExitCode == 6) && res.Stats.FilesTransferred+res.Stats.FilesDeleted > 0 {
			res.Warning = T("some files were not copied: see the log")
			return res, nil
		}
		if decryptErr || (c.encrypted && isDecryptError(lastErr)) {
			return res, errors.New(T("the files cannot be decrypted: check the password (it must be the same used to make the copy)"))
		}
		if lastErr == "" {
			lastErr = Tf("rclone exit code %d", res.ExitCode)
		}
		return res, errors.New(lastErr)
	}
	return res, err
}

// isDecryptError recognises the errors rclone gives when encrypted files are
// read with the wrong password.
func isDecryptError(msg string) bool {
	return strings.Contains(msg, "unexpected EOF") || strings.Contains(msg, "failed to authenticate decrypted block") ||
		strings.Contains(msg, "bad password")
}

// listFiles returns the files already in the destination (for the dry run).
func (c *Config) listFiles(ctx context.Context, opts Options) (map[string]bool, error) {
	out, err := c.run(ctx, "lsf", "-R", "--files-only", "--exclude", "/"+archiveDirName+"/**", opts.Dst)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			set[l] = true
		}
	}
	return set, nil
}

func progress(s jsonStats, files int) syncer.Progress {
	p := syncer.Progress{Bytes: s.Bytes, Files: files, Speed: humanSpeed(s.Speed)}
	if s.TotalBytes > 0 {
		p.Percent = int(s.Bytes * 100 / s.TotalBytes)
	}
	p.ETA = "-"
	if s.ETA != nil {
		d := time.Duration(*s.ETA) * time.Second
		p.ETA = fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	if len(s.Transferring) > 0 {
		p.Current = s.Transferring[0].Name
	}
	return p
}

func humanSpeed(b float64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.2fMB/s", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2fkB/s", b/(1<<10))
	}
	return fmt.Sprintf("%.0fB/s", b)
}
