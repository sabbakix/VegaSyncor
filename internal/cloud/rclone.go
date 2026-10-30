// Package cloud copies to OneDrive with rclone.
//
// The OneDrive sign-in token is kept encrypted in the VegaSyncor configuration:
// for every operation a temporary rclone configuration file (0600, in the runtime
// folder) is written with the token, and removed afterwards. rclone refreshes the
// token when it expires and writes it back to that file: Token reads it so that
// the service can store the new one.
package cloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

// Binary is the rclone command.
var Binary = "rclone"

// Available reports whether rclone is installed.
func Available() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return errors.New(T("rclone is not installed (apt install rclone)"))
	}
	return nil
}

// Remote is a OneDrive drive, or a local folder simulating it (DevRoot, dev mode).
type Remote struct {
	DriveID   string
	DriveType string
	Token     string // JSON of the OAuth token
	DevRoot   string
}

// Crypt enables client-side encryption (rclone crypt) under the destination.
type Crypt struct {
	Password string
	Names    bool // encrypt file and folder names too
}

// Config is a temporary rclone configuration.
type Config struct {
	path string
	base string // drive-absolute path prefix in the "od" remote ("" or the dev root)
	// Root is the rclone path of the OneDrive folder of the job (the encrypted
	// remote over it if Crypt): the destination of an upload, or the source of a
	// download.
	Root string
	// Raw is the OneDrive folder itself (== Root without encryption).
	Raw       string
	encrypted bool
}

// NewConfig writes the rclone configuration for the drive in dir. root is the
// folder of the job on the drive; with crypt the files under it are encrypted.
func NewConfig(dir string, r Remote, root string, crypt *Crypt) (*Config, error) {
	if strings.ContainsAny(r.Token+r.DriveID+r.DriveType, "\r\n") {
		return nil, errors.New("invalid OneDrive token")
	}
	var b strings.Builder
	c := &Config{}
	if r.DevRoot != "" {
		b.WriteString("[od]\ntype = local\n")
		c.base = r.DevRoot
	} else {
		fmt.Fprintf(&b, "[od]\ntype = onedrive\ntoken = %s\ndrive_id = %s\ndrive_type = %s\n", r.Token, r.DriveID, r.DriveType)
	}
	c.Root = c.Abs(root)
	c.Raw = c.Root
	if crypt != nil {
		pass, err := Obscure(crypt.Password)
		if err != nil {
			return nil, err
		}
		names, dirs := "off", "false"
		if crypt.Names {
			names, dirs = "standard", "true"
		}
		fmt.Fprintf(&b, "\n[enc]\ntype = crypt\nremote = %s\npassword = %s\nfilename_encryption = %s\ndirectory_name_encryption = %s\n",
			c.Root, pass, names, dirs)
		c.Root, c.encrypted = "enc:", true
	}
	f, err := os.CreateTemp(dir, "rclone-*.conf")
	if err != nil {
		return nil, err
	}
	c.path = f.Name()
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		c.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Close removes the configuration file (it holds the token).
func (c *Config) Close() {
	if c != nil && c.path != "" {
		os.Remove(c.path)
	}
}

// Abs is the rclone path of a folder given from the root of the drive.
func (c *Config) Abs(p string) string {
	p = strings.Trim(p, "/")
	if c.base != "" {
		return "od:" + path.Join(c.base, p)
	}
	return "od:" + p
}

// Join appends a relative path to an rclone path.
func Join(remote, sub string) string {
	sub = strings.Trim(sub, "/")
	switch {
	case sub == "":
		return remote
	case strings.HasSuffix(remote, ":"):
		return remote + sub
	}
	return strings.TrimSuffix(remote, "/") + "/" + sub
}

// Token returns the token currently in the configuration file (rclone writes a
// new one there when it refreshes it).
func (c *Config) Token() string {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return ""
	}
	section := ""
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			section = l
		} else if k, v, ok := strings.Cut(l, "="); ok && section == "[od]" && strings.TrimSpace(k) == "token" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (c *Config) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, Binary, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "RCLONE_CONFIG="+c.path, "RCLONE_CONFIG_PASS=")
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 15 * time.Second
	return cmd
}

// run executes rclone and returns its standard output; the error carries the
// last line of the error output.
func (c *Config) run(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := c.command(ctx, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := lastLine(stderr.String()); msg != "" {
			return out, errors.New(msg)
		}
		return out, err
	}
	return out, nil
}

// Obscure turns a password into the form rclone expects in its configuration.
func Obscure(pass string) (string, error) {
	cmd := exec.Command(Binary, "obscure", "-")
	cmd.Stdin = strings.NewReader(pass)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rclone obscure: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ListDirs lists the folders inside remote (the archive folder is hidden).
func (c *Config) ListDirs(ctx context.Context, remote string) ([]string, error) {
	out, err := c.run(ctx, "lsjson", "--dirs-only", "--no-modtime", "--no-mimetype", remote)
	if err != nil {
		return nil, err
	}
	var items []struct {
		Name  string
		IsDir bool
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}
	dirs := []string{}
	for _, it := range items {
		if it.IsDir && it.Name != archiveDirName {
			dirs = append(dirs, it.Name)
		}
	}
	return dirs, nil
}

// IsEmpty reports whether a folder has no files or folders (the archive folder
// does not count); a missing folder is an error.
func (c *Config) IsEmpty(ctx context.Context, remote string) (bool, error) {
	out, err := c.run(ctx, "lsf", "--max-depth", "1", "--exclude", "/"+archiveDirName+"/**", remote)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// Mkdir creates a folder.
func (c *Config) Mkdir(ctx context.Context, remote string) error {
	_, err := c.run(ctx, "mkdir", remote)
	return err
}

// Space is the quota of the drive, in bytes.
type Space struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// About returns the quota of the drive.
func (c *Config) About(ctx context.Context) (Space, error) {
	var s Space
	out, err := c.run(ctx, "about", "--json", c.Abs(""))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(out, &s)
}

// PruneArchive removes the dated folders older than days days from the archive
// folder (only folders named like a run stamp are touched).
func (c *Config) PruneArchive(ctx context.Context, archive string, days int, now time.Time) ([]string, error) {
	if days <= 0 {
		return nil, nil
	}
	out, err := c.run(ctx, "lsf", "--dirs-only", archive)
	if err != nil {
		if strings.Contains(err.Error(), "directory not found") {
			return nil, nil
		}
		return nil, err
	}
	limit := now.AddDate(0, 0, -days)
	var removed []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSuffix(sc.Text(), "/")
		t, err := time.ParseInLocation(stampFormat, name, now.Location())
		if err != nil || !t.Before(limit) {
			continue
		}
		if _, err := c.run(ctx, "purge", Join(archive, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// stripStamp removes the date and time rclone puts before its plain-text messages
// ("2026/10/30 11:24:30 Failed to ...").
func stripStamp(l string) string {
	if len(l) > 20 && l[4] == '/' && l[7] == '/' && l[10] == ' ' {
		l = strings.TrimSpace(l[20:])
	}
	return strings.TrimPrefix(l, "ERROR : ")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		var j struct {
			Msg string `json:"msg"`
		}
		if json.Unmarshal([]byte(l), &j) == nil && j.Msg != "" {
			return j.Msg
		}
		return stripStamp(l)
	}
	return ""
}
