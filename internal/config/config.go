// Package config defines connections, sync jobs and schedules.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Connection describes an SMB/CIFS server and its credentials.
type Connection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Domain      string `json:"domain,omitempty"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc,omitempty"`
	// SMBVersion: "" = automatic negotiation, otherwise "3.1.1", "3.0", "2.1", "2.0", "1.0".
	SMBVersion string `json:"smb_version,omitempty"`
}

const (
	LocSMB   = "smb"
	LocLocal = "local"
)

// Location is a folder: local to the server or on an SMB share.
type Location struct {
	Type         string `json:"type"`
	ConnectionID string `json:"connection_id,omitempty"`
	Share        string `json:"share,omitempty"`
	Path         string `json:"path,omitempty"`
}

const (
	ModeMirror        = "mirror"
	ModeMirrorArchive = "mirror_archive"
	ModeAdditive      = "additive"
)

var Modes = []string{ModeMirrorArchive, ModeMirror, ModeAdditive}

func ModeLabel(m string) string {
	switch m {
	case ModeMirror:
		return T("Mirror")
	case ModeMirrorArchive:
		return T("Mirror + archive")
	case ModeAdditive:
		return T("Add only")
	}
	return m
}

func ModeDescription(m string) string {
	switch m {
	case ModeMirror:
		return T("The destination becomes identical to the source: files deleted at the source are deleted.")
	case ModeMirrorArchive:
		return Tf("Like Mirror, but deleted or overwritten files are moved to %s/<date>.", ArchiveDirName)
	case ModeAdditive:
		return T("Copies new and changed files, never deletes anything in the destination.")
	}
	return ""
}

// ArchiveDirName is the folder (in the destination root) holding archived versions.
const ArchiveDirName = ".vegasyncor-archive"

type Job struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Source      Location `json:"source"`
	SourceRO    bool     `json:"source_readonly"`
	Dest        Location `json:"dest"`
	Mode        string   `json:"mode"`
	ArchiveDays int      `json:"archive_days,omitempty"`
	Schedule    Schedule `json:"schedule"`
	Excludes    []string `json:"excludes,omitempty"`
	// AllowEmptySource allows a mirror even if the source is empty
	// (normally blocked so the destination is not wiped by mistake).
	AllowEmptySource bool `json:"allow_empty_source,omitempty"`
	// BandwidthKBps limits the bandwidth in KB/s (0 = unlimited).
	BandwidthKBps int `json:"bandwidth_kbps,omitempty"`
}

type Config struct {
	MaxParallel int `json:"max_parallel"`
	// Language of the interface and of the service messages ("en", "it").
	Language    string       `json:"language,omitempty"`
	Connections []Connection `json:"connections"`
	Jobs        []Job        `json:"jobs"`
}

func Default() *Config {
	return &Config{MaxParallel: 2}
}

func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.MaxParallel < 1 {
		c.MaxParallel = 1
	}
	return c, nil
}

// Save writes the configuration atomically (temporary file + rename).
func (c *Config) Save(path string) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) Connection(id string) *Connection {
	for i := range c.Connections {
		if c.Connections[i].ID == id {
			return &c.Connections[i]
		}
	}
	return nil
}

func (c *Config) Job(id string) *Job {
	for i := range c.Jobs {
		if c.Jobs[i].ID == id {
			return &c.Jobs[i]
		}
	}
	return nil
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9._:\-\[\]]+$`)

func (cn *Connection) Validate() error {
	cn.Name = strings.TrimSpace(cn.Name)
	cn.Host = strings.Trim(strings.TrimSpace(cn.Host), `\/`)
	cn.Username = strings.TrimSpace(cn.Username)
	cn.Domain = strings.TrimSpace(cn.Domain)
	if cn.Name == "" {
		return errors.New(T("the connection name is required"))
	}
	if cn.Host == "" || !hostRe.MatchString(cn.Host) {
		return errors.New(T("invalid host (e.g. 192.168.1.10 or server01)"))
	}
	if strings.ContainsAny(cn.Username+cn.Domain, ",\n") {
		return errors.New(T("user or domain contain characters that are not allowed"))
	}
	switch cn.SMBVersion {
	case "", "3.1.1", "3.0", "2.1", "2.0", "1.0":
	default:
		return errors.New(T("invalid SMB version"))
	}
	return nil
}

// CleanSubPath normalises a relative path inside a share.
func CleanSubPath(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New(T("the path cannot leave the share"))
	}
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

func (l *Location) validate(c *Config, what string) error {
	switch l.Type {
	case LocSMB:
		if c.Connection(l.ConnectionID) == nil {
			return errors.New(what + ": " + T("select a connection"))
		}
		l.Share = strings.Trim(strings.TrimSpace(l.Share), `\/`)
		if l.Share == "" || strings.ContainsAny(l.Share, `/\,`) {
			return errors.New(what + ": " + T("invalid share name"))
		}
		p, err := CleanSubPath(l.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		l.Path = p
	case LocLocal:
		l.ConnectionID, l.Share = "", ""
		l.Path = filepath.Clean(strings.TrimSpace(l.Path))
		if !filepath.IsAbs(l.Path) {
			return errors.New(what + ": " + T("the local path must be absolute"))
		}
		if l.Path == "/" {
			return errors.New(what + ": " + T("the root / cannot be used"))
		}
	default:
		return errors.New(what + ": " + T("unknown type"))
	}
	return nil
}

// Display returns a readable representation of the location.
func (l Location) Display(c *Config) string {
	switch l.Type {
	case LocSMB:
		host := "?"
		if cn := c.Connection(l.ConnectionID); cn != nil {
			host = cn.Host
		}
		s := `\\` + host + `\` + l.Share
		if l.Path != "" {
			s += `\` + strings.ReplaceAll(l.Path, "/", `\`)
		}
		return s
	case LocLocal:
		return l.Path
	}
	return "?"
}

func (j *Job) Validate(c *Config) error {
	j.Name = strings.TrimSpace(j.Name)
	if j.Name == "" {
		return errors.New(T("the job name is required"))
	}
	if err := j.Source.validate(c, T("source")); err != nil {
		return err
	}
	if err := j.Dest.validate(c, T("destination")); err != nil {
		return err
	}
	if j.Source == j.Dest {
		return errors.New(T("source and destination are the same"))
	}
	if j.Source.Type == LocLocal && j.Dest.Type == LocLocal {
		s, d := j.Source.Path+"/", j.Dest.Path+"/"
		if strings.HasPrefix(d, s) || strings.HasPrefix(s, d) {
			return errors.New(T("source and destination cannot be nested"))
		}
	}
	switch j.Mode {
	case ModeMirror, ModeAdditive:
	case ModeMirrorArchive:
		if j.ArchiveDays < 0 {
			return errors.New(T("invalid number of archive days"))
		}
	default:
		return errors.New(T("invalid mode"))
	}
	if j.BandwidthKBps < 0 {
		return errors.New(T("invalid bandwidth limit"))
	}
	var ex []string
	for _, e := range j.Excludes {
		if e = strings.TrimSpace(e); e != "" {
			ex = append(ex, e)
		}
	}
	j.Excludes = ex
	return j.Schedule.Validate()
}

// DestConflict returns another job whose destination is the same folder as j's, or
// contains it / is contained in it, when at least one of the two is a mirror: each
// run would delete (or archive) the other job's files. Two "Add only" jobs may
// share a folder. Returns nil if there is no conflict.
func (c *Config) DestConflict(j Job) *Job {
	for i := range c.Jobs {
		o := &c.Jobs[i]
		if o.ID == j.ID || (!isMirror(j.Mode) && !isMirror(o.Mode)) {
			continue
		}
		if c.locationsOverlap(j.Dest, o.Dest) {
			return o
		}
	}
	return nil
}

// DestConflicts lists the pairs of jobs already in the configuration whose
// destinations conflict (see DestConflict), each pair once.
func (c *Config) DestConflicts() [][2]*Job {
	var out [][2]*Job
	for i := range c.Jobs {
		for k := i + 1; k < len(c.Jobs); k++ {
			a, b := &c.Jobs[i], &c.Jobs[k]
			if (isMirror(a.Mode) || isMirror(b.Mode)) && c.locationsOverlap(a.Dest, b.Dest) {
				out = append(out, [2]*Job{a, b})
			}
		}
	}
	return out
}

func isMirror(mode string) bool { return mode == ModeMirror || mode == ModeMirrorArchive }

// locationsOverlap reports whether a and b are the same folder or one contains
// the other. SMB locations are compared by host and share (not by connection: two
// connections with different users can point to the same server), case-insensitively
// like Windows and Samba; local paths are compared exactly.
func (c *Config) locationsOverlap(a, b Location) bool {
	if a.Type != b.Type {
		return false
	}
	pa, pb := a.Path, b.Path
	if a.Type == LocSMB {
		ca, cb := c.Connection(a.ConnectionID), c.Connection(b.ConnectionID)
		if ca == nil || cb == nil || !strings.EqualFold(ca.Host, cb.Host) || !strings.EqualFold(a.Share, b.Share) {
			return false
		}
		pa, _ = CleanSubPath(pa)
		pb, _ = CleanSubPath(pb)
		// leading "/" so that the share root ("") contains every subfolder
		pa, pb = "/"+strings.ToLower(pa), "/"+strings.ToLower(pb)
	} else {
		pa, pb = filepath.Clean(pa), filepath.Clean(pb)
	}
	pa, pb = strings.TrimSuffix(pa, "/")+"/", strings.TrimSuffix(pb, "/")+"/"
	return strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa)
}
