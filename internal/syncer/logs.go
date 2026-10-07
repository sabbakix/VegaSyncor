package syncer

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Copies of the run logs in a folder chosen by the user: one file per run,
// <job>_<stamp>.log (<job>_<stamp>_dry-run.log for dry runs). Logs older than N
// days are moved into one zip per month, <job>_logs_<YYYY-MM>.zip, which opens
// natively on Windows too.

// SafeName turns a job name into a file name valid on Linux and Windows shares.
func SafeName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			r = '_'
		}
		b.WriteRune(r)
	}
	s := strings.TrimRight(b.String(), ". ")
	if s == "" {
		s = "job"
	}
	return s
}

// LogFileName is the name of the copy of a run log.
func LogFileName(job, stamp string, dry bool) string {
	name := SafeName(job) + "_" + stamp
	if dry {
		name += "_dry-run"
	}
	return name + ".log"
}

// SaveLog copies the log file src into dir and returns the path of the copy.
func SaveLog(dir, job, stamp string, dry bool, src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dst := filepath.Join(dir, LogFileName(job, stamp, dry))
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// CompressLogs moves the logs of job older than days days into monthly zip
// files and returns how many logs were compressed. Other files are not touched.
func CompressLogs(dir, job string, days int, now time.Time) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	safe := SafeName(job)
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(safe) + `_(\d{4}-\d{2}-\d{2}_\d{6})(_dry-run)?\.log$`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	limit := now.AddDate(0, 0, -days)
	months := map[string][]string{}
	for _, e := range entries {
		m := re.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		t, err := time.ParseInLocation(StampFormat, m[1], now.Location())
		if err != nil || !t.Before(limit) {
			continue
		}
		month := t.Format("2006-01")
		months[month] = append(months[month], e.Name())
	}
	n := 0
	var errs []error
	for month, files := range months {
		sort.Strings(files)
		zipPath := filepath.Join(dir, safe+"_logs_"+month+".zip")
		if err := addToZip(zipPath, dir, files); err != nil {
			errs = append(errs, err)
			continue
		}
		for _, f := range files {
			if err := os.Remove(filepath.Join(dir, f)); err != nil {
				errs = append(errs, err)
			}
		}
		n += len(files)
	}
	return n, errors.Join(errs...)
}

// addToZip writes zipPath with its current entries plus files (from dir); the
// new archive replaces the old one only when it is complete.
func addToZip(zipPath, dir string, files []string) error {
	tmp := zipPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(tmp)
		}
	}()
	w := zip.NewWriter(out)
	have := map[string]bool{}
	if r, err := zip.OpenReader(zipPath); err == nil {
		for _, f := range r.File {
			if err := w.Copy(f); err != nil {
				r.Close()
				return err
			}
			have[f.Name] = true
		}
		r.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range files {
		if have[name] {
			continue // already archived (e.g. an interrupted previous compression)
		}
		if err := addFile(w, filepath.Join(dir, name), name); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return os.Rename(tmp, zipPath)
}

func addFile(w *zip.Writer, path, name string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(st)
	if err != nil {
		return err
	}
	h.Name, h.Method = name, zip.Deflate
	fw, err := w.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, in)
	return err
}
