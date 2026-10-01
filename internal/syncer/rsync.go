// Package syncer esegue la copia vera e propria tramite rsync.
package syncer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"vegasyncor/internal/config"
)

// Options descrive una singola esecuzione di rsync.
type Options struct {
	Src, Dst      string // directory già montate/accessibili
	Mode          string
	Excludes      []string
	DryRun        bool
	BandwidthKBps int
	RunStamp      string // usato per la cartella di archivio
}

type Progress struct {
	Bytes   int64   `json:"bytes"`
	Percent int     `json:"percent"`
	Speed   string  `json:"speed"`
	ETA     string  `json:"eta"`
	Files   int     `json:"files"`   // file processati finora (itemize)
	Current string  `json:"current"` // ultimo file
	Ratio   float64 `json:"-"`
}

type Stats struct {
	FilesTotal       int64 `json:"files_total"`
	FilesTransferred int64 `json:"files_transferred"`
	FilesDeleted     int64 `json:"files_deleted"`
	BytesTotal       int64 `json:"bytes_total"`
	BytesTransferred int64 `json:"bytes_transferred"`
}

// DefaultExcludes sono file di sistema Windows/macOS che non ha senso copiare.
var DefaultExcludes = []string{"Thumbs.db", "desktop.ini", "~$*", ".DS_Store", "$RECYCLE.BIN/", "System Volume Information/"}

func BuildArgs(o Options) []string {
	args := []string{
		"-rt",                // ricorsivo + date (permessi/proprietari non hanno senso tra SMB e Linux)
		"--modify-window=2",  // tolleranza su timestamp FAT/SMB
		"--no-inc-recursive", // percentuale di avanzamento affidabile
		"-i", "--info=progress2", "--stats",
		"--partial-dir=.vegasyncor-partial",
		"--exclude=/" + config.ArchiveDirName + "/",
		"--exclude=.vegasyncor-partial/",
	}
	for _, e := range DefaultExcludes {
		args = append(args, "--exclude="+e)
	}
	for _, e := range o.Excludes {
		args = append(args, "--exclude="+e)
	}
	switch o.Mode {
	case config.ModeMirror:
		args = append(args, "--delete", "--delete-delay")
	case config.ModeMirrorArchive:
		args = append(args, "--delete", "--delete-delay", "--backup",
			"--backup-dir="+filepath.Join(o.Dst, config.ArchiveDirName, o.RunStamp))
	}
	if o.DryRun {
		args = append(args, "--dry-run")
	}
	if o.BandwidthKBps > 0 {
		args = append(args, fmt.Sprintf("--bwlimit=%d", o.BandwidthKBps))
	}
	return append(args, strings.TrimSuffix(o.Src, "/")+"/", strings.TrimSuffix(o.Dst, "/")+"/")
}

var (
	progressRe = regexp.MustCompile(`^\s*([\d,.]+)\s+(\d+)%\s+(\S+)\s+(\d+:\d+:\d+)`)
	itemizeRe  = regexp.MustCompile(`^([<>ch.*][fdLDS][^ ]{9}|\*deleting) +(.+)$`)
	statRes    = map[string]*regexp.Regexp{
		"files_total":       regexp.MustCompile(`^Number of files: ([\d,]+)`),
		"files_transferred": regexp.MustCompile(`^Number of regular files transferred: ([\d,]+)`),
		"files_deleted":     regexp.MustCompile(`^Number of deleted files: ([\d,]+)`),
		"bytes_total":       regexp.MustCompile(`^Total file size: ([\d,]+)`),
		"bytes_transferred": regexp.MustCompile(`^Total transferred file size: ([\d,]+)`),
	}
)

func num(s string) int64 {
	n, _ := strconv.ParseInt(strings.NewReplacer(",", "", ".", "").Replace(s), 10, 64)
	return n
}

// Result è l'esito di un'esecuzione di rsync.
type Result struct {
	Stats    Stats
	ExitCode int
	Warning  string // per codici di uscita "parziali" (23/24)
}

// Run esegue rsync scrivendo l'elenco delle modifiche su log e
// notificando l'avanzamento tramite onProgress.
func Run(ctx context.Context, o Options, log io.Writer, onProgress func(Progress)) (Result, error) {
	args := BuildArgs(o)
	fmt.Fprintf(log, "# rsync %s\n", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	var mu sync.Mutex
	cmd.Stderr = io.MultiWriter(&lockedWriter{w: log, mu: &mu}, &stderr)
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var res Result
	var prog Progress
	lastNotify := time.Time{}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(splitCRLF)
	for sc.Scan() {
		line := sc.Text()
		if m := progressRe.FindStringSubmatch(line); m != nil {
			prog.Bytes = num(m[1])
			prog.Percent, _ = strconv.Atoi(m[2])
			prog.Speed = m[3]
			prog.ETA = m[4]
		} else if strings.TrimSpace(line) == "" {
			continue
		} else {
			mu.Lock()
			fmt.Fprintln(log, line)
			mu.Unlock()
			if m := itemizeRe.FindStringSubmatch(line); m != nil {
				prog.Files++
				prog.Current = m[2]
			}
			for k, re := range statRes {
				if m := re.FindStringSubmatch(line); m != nil {
					v := num(m[1])
					switch k {
					case "files_total":
						res.Stats.FilesTotal = v
					case "files_transferred":
						res.Stats.FilesTransferred = v
					case "files_deleted":
						res.Stats.FilesDeleted = v
					case "bytes_total":
						res.Stats.BytesTotal = v
					case "bytes_transferred":
						res.Stats.BytesTransferred = v
					}
				}
			}
		}
		if onProgress != nil && time.Since(lastNotify) > 300*time.Millisecond {
			lastNotify = time.Now()
			onProgress(prog)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return res, context.Canceled
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		switch res.ExitCode {
		case 23:
			res.Warning = "alcuni file non sono stati copiati (bloccati o senza permessi): vedere il log"
			return res, nil
		case 24:
			res.Warning = "alcuni file sono spariti durante la copia"
			return res, nil
		}
		return res, fmt.Errorf("rsync codice %d: %s", res.ExitCode, lastLine(stderr.String()))
	}
	return res, err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" && !strings.HasPrefix(l, "rsync error:") {
			return l
		}
	}
	if len(lines) > 0 {
		return lines[len(lines)-1]
	}
	return ""
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// splitCRLF divide l'output sia su \n che su \r (rsync aggiorna il progresso con \r).
func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// IsEmptyDir indica se una directory non contiene alcuna voce.
func IsEmptyDir(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}

// PruneArchive rimuove le cartelle di archivio più vecchie di days giorni.
func PruneArchive(dst string, days int, now time.Time) ([]string, error) {
	if days <= 0 {
		return nil, nil
	}
	base := filepath.Join(dst, config.ArchiveDirName)
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	limit := now.AddDate(0, 0, -days)
	var removed []string
	for _, e := range entries {
		t, err := time.ParseInLocation(StampFormat, e.Name(), now.Location())
		if err != nil || !e.IsDir() || !t.Before(limit) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(base, e.Name())); err != nil {
			return removed, err
		}
		removed = append(removed, e.Name())
	}
	return removed, nil
}

const StampFormat = "2006-01-02_150405"
