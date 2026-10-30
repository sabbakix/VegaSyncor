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
	"slices"
	"strings"
)

// Connection describes an SMB/CIFS server and its credentials, or a OneDrive
// account (Kind ConnOneDrive) whose sign-in token is kept in PasswordEnc.
type Connection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"` // "" = SMB, ConnOneDrive
	Host        string `json:"host,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc,omitempty"`
	// SMBVersion: "" = automatic negotiation, otherwise "3.1.1", "3.0", "2.1", "2.0", "1.0".
	SMBVersion string `json:"smb_version,omitempty"`

	// OneDrive: account shown to the user, and the drive the files go to.
	Account   string `json:"account,omitempty"`
	DriveID   string `json:"drive_id,omitempty"`
	DriveType string `json:"drive_type,omitempty"` // personal, business, documentLibrary
}

// ConnOneDrive is the kind of the OneDrive connections (off-site copies).
const ConnOneDrive = "onedrive"

func (cn *Connection) IsOneDrive() bool { return cn.Kind == ConnOneDrive }

const (
	LocSMB      = "smb"
	LocLocal    = "local"
	LocOneDrive = "onedrive" // destination only
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
	// Checksum compares the contents of the files (rsync --checksum) instead of
	// size and modification time: detects every change but reads all the files
	// on both sides at every run.
	Checksum bool `json:"checksum,omitempty"`
	// Archive is where Mirror + archive moves deleted and overwritten files
	// (a dated subfolder per run); nil = ArchiveDirName inside the destination.
	Archive *Location `json:"archive,omitempty"`
	// LogDir receives a copy of the log of every run (nil = not saved).
	LogDir *Location `json:"log_dir,omitempty"`
	// LogCompressDays: logs in LogDir older than this are moved into monthly
	// zip files (0 = never).
	LogCompressDays int `json:"log_compress_days,omitempty"`

	// Encrypt encrypts the files before they are uploaded to OneDrive (rclone
	// crypt), with the password in CryptPassEnc (encrypted with the master key).
	Encrypt      bool   `json:"encrypt,omitempty"`
	EncryptNames bool   `json:"encrypt_names,omitempty"` // file and folder names too
	CryptPassEnc string `json:"crypt_password_enc,omitempty"`
	// CryptPassword is only sent by the TUI when the password is set or
	// changed; the service encrypts it into CryptPassEnc and never stores it.
	CryptPassword string `json:"crypt_password,omitempty"`
}

// CryptAAD binds the encrypted encryption password of a job to the job.
func CryptAAD(jobID string) string { return "crypt:" + jobID }

// ConnectionIDs lists the connections used by the job (each once).
func (j Job) ConnectionIDs() []string {
	var out []string
	for _, l := range []*Location{&j.Source, &j.Dest, j.Archive, j.LogDir} {
		if l != nil && l.Type != LocLocal && l.ConnectionID != "" && !slices.Contains(out, l.ConnectionID) {
			out = append(out, l.ConnectionID)
		}
	}
	return out
}

type Config struct {
	MaxParallel int `json:"max_parallel"`
	// Language of the interface and of the service messages ("en", "it").
	Language string `json:"language,omitempty"`
	// Firewall managed by VegaSyncor (nftables).
	Firewall    Firewall     `json:"firewall"`
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
	if cn.IsOneDrive() {
		cn.Host, cn.Username, cn.Domain, cn.SMBVersion = "", "", "", ""
		if cn.Name == "" {
			return errors.New(T("the connection name is required"))
		}
		switch cn.DriveType {
		case "personal", "business", "documentLibrary":
		default:
			return errors.New(T("OneDrive: sign in to choose the account"))
		}
		if cn.DriveID == "" || strings.ContainsAny(cn.DriveID, "\r\n") {
			return errors.New(T("OneDrive: sign in to choose the account"))
		}
		return nil
	}
	if cn.Kind != "" {
		return errors.New(T("unknown connection type"))
	}
	cn.Account, cn.DriveID, cn.DriveType = "", "", ""
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
		if cn := c.Connection(l.ConnectionID); cn == nil || cn.IsOneDrive() {
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
	case LocOneDrive:
		if cn := c.Connection(l.ConnectionID); cn == nil || !cn.IsOneDrive() {
			return errors.New(what + ": " + T("select a OneDrive connection"))
		}
		l.Share = ""
		p, err := CleanSubPath(l.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		l.Path = p
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
	case LocOneDrive:
		name := "?"
		if cn := c.Connection(l.ConnectionID); cn != nil {
			name = cn.Name
		}
		return "OneDrive " + name + ":/" + l.Path
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
	if j.Source.Type == LocOneDrive {
		return errors.New(T("OneDrive can only be used as destination"))
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
	if j.Mode != ModeMirrorArchive {
		j.Archive = nil
	}
	if err := j.validateFolder(c, j.Archive, T("deleted items folder")); err != nil {
		return err
	}
	if err := j.validateFolder(c, j.LogDir, T("log folder")); err != nil {
		return err
	}
	if j.LogDir == nil {
		j.LogCompressDays = 0
	}
	if j.LogCompressDays < 0 {
		return errors.New(T("invalid number of days for the log compression"))
	}
	if err := j.validateCloud(c); err != nil {
		return err
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

// validateCloud checks the settings that depend on a OneDrive destination.
func (j *Job) validateCloud(c *Config) error {
	if j.LogDir != nil && j.LogDir.Type == LocOneDrive {
		return errors.New(T("log folder: choose a folder of this server or a network folder"))
	}
	if j.Dest.Type != LocOneDrive {
		if j.Archive != nil && j.Archive.Type == LocOneDrive {
			return errors.New(T("deleted items folder: OneDrive can be used only with a OneDrive destination"))
		}
		j.Encrypt, j.EncryptNames, j.CryptPassEnc, j.CryptPassword = false, false, "", ""
		return nil
	}
	if a := j.Archive; a != nil && (a.Type != LocOneDrive || a.ConnectionID != j.Dest.ConnectionID) {
		return errors.New(T("deleted items folder: with a OneDrive destination it must be on the same OneDrive"))
	}
	if j.Encrypt {
		if j.Archive != nil {
			return errors.New(T("with encryption the deleted items are kept in the archive folder of the destination: leave the deleted items folder on default"))
		}
		if j.Checksum {
			return errors.New(T("encrypted files cannot be compared by content: turn off 'Compare contents'"))
		}
		if j.CryptPassword == "" && j.CryptPassEnc == "" {
			return errors.New(T("encryption: enter the password"))
		}
	} else {
		j.EncryptNames, j.CryptPassEnc, j.CryptPassword = false, "", ""
	}
	return nil
}

// validateFolder checks a folder written by the job besides the destination
// (archive, logs): it must not touch the source (which is protected), and it
// must not be the destination or contain it. Inside the destination it is
// allowed: the sync skips it.
func (j *Job) validateFolder(c *Config, l *Location, what string) error {
	if l == nil {
		return nil
	}
	if err := l.validate(c, what); err != nil {
		return err
	}
	if c.locationsOverlap(*l, j.Source) {
		return errors.New(what + ": " + T("it cannot be in the source or contain it"))
	}
	if c.Within(j.Dest, *l) {
		return errors.New(what + ": " + T("it cannot be the destination or contain it (choose a subfolder or another folder)"))
	}
	return nil
}

// Within reports whether child is the folder parent or one of its subfolders
// (same rules as locationsOverlap).
func (c *Config) Within(child, parent Location) bool {
	_, ok := c.RelPath(child, parent)
	return ok
}

// locRoot identifies the storage a location is on, and its path there. SMB
// locations are compared by host and share (not by connection: two connections
// with different users can point to the same server) and OneDrive ones by drive,
// case-insensitively like Windows; local paths are compared exactly.
func (c *Config) locRoot(l Location) (root, p string, fold, ok bool) {
	switch l.Type {
	case LocSMB:
		cn := c.Connection(l.ConnectionID)
		if cn == nil {
			return "", "", false, false
		}
		p, _ = CleanSubPath(l.Path)
		return "smb:" + strings.ToLower(cn.Host) + "/" + strings.ToLower(l.Share), p, true, true
	case LocOneDrive:
		cn := c.Connection(l.ConnectionID)
		if cn == nil {
			return "", "", false, false
		}
		p, _ = CleanSubPath(l.Path)
		return "onedrive:" + strings.ToLower(cn.DriveID), p, true, true
	case LocLocal:
		return "local", filepath.Clean(l.Path), false, true
	}
	return "", "", false, false
}

// RelPath returns the path of child relative to parent ("" when they are the
// same folder) if child is parent or inside it.
func (c *Config) RelPath(child, parent Location) (string, bool) {
	rc, pc, fold, ok1 := c.locRoot(child)
	rp, pp, _, ok2 := c.locRoot(parent)
	if !ok1 || !ok2 || rc != rp {
		return "", false
	}
	if !fold {
		rel, err := filepath.Rel(pp, pc)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", false
		}
		if rel == "." {
			rel = ""
		}
		return rel, true
	}
	switch {
	case pp == "":
		return pc, true
	case strings.EqualFold(pc, pp):
		return "", true
	case len(pc) > len(pp) && strings.EqualFold(pc[:len(pp)+1], pp+"/"):
		return pc[len(pp)+1:], true
	}
	return "", false
}

// FolderConflict checks the archive and log folders of j against the other
// jobs: a folder inside the destination of another mirror job would be deleted
// by it, and two jobs cannot share an archive folder (each one cleans it with
// its own number of days). Returns "" if there is no conflict.
func (c *Config) FolderConflict(j Job) string {
	for i := range c.Jobs {
		o := &c.Jobs[i]
		if o.ID == j.ID {
			continue
		}
		for _, f := range []struct {
			l    *Location
			what string
		}{{j.Archive, T("deleted items folder")}, {j.LogDir, T("log folder")}} {
			if f.l != nil && isMirror(o.Mode) && c.locationsOverlap(*f.l, o.Dest) {
				return Tf("%s: it overlaps with the destination of the job %q, whose mirror would delete it", f.what, o.Name)
			}
		}
		for _, l := range []*Location{o.Archive, o.LogDir} {
			if l != nil && isMirror(j.Mode) && c.locationsOverlap(*l, j.Dest) {
				return Tf("the destination overlaps with a folder used by the job %q (deleted items or logs): the mirror would delete it", o.Name)
			}
		}
		if j.Archive != nil && o.Archive != nil && c.locationsOverlap(*j.Archive, *o.Archive) {
			return Tf("deleted items folder: it is already used by the job %q", o.Name)
		}
	}
	return ""
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
// the other (see RelPath for how folders are compared).
func (c *Config) locationsOverlap(a, b Location) bool {
	return c.Within(a, b) || c.Within(b, a)
}

// CleanFolderName validates the name of a new folder: it must be a single
// folder name that is also valid on Windows shares.
func CleanFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New(T("enter the folder name"))
	case name == "." || name == "..":
		return "", errors.New(T("invalid folder name"))
	case strings.ContainsAny(name, `/\:*?"<>|`):
		return "", errors.New(T(`the folder name cannot contain / \ : * ? " < > |`))
	case strings.HasSuffix(name, "."):
		return "", errors.New(T("the folder name cannot end with a dot"))
	case len(name) > 200:
		return "", errors.New(T("the folder name is too long"))
	}
	for _, r := range name {
		if r < 32 {
			return "", errors.New(T("invalid folder name"))
		}
	}
	return name, nil
}
