// Package config definisce connessioni, job di sincronizzazione e pianificazioni.
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

// Connection descrive un server SMB/CIFS con le relative credenziali.
type Connection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Domain      string `json:"domain,omitempty"`
	Username    string `json:"username"`
	PasswordEnc string `json:"password_enc,omitempty"`
	// SMBVersion: "" = negoziazione automatica, altrimenti "3.1.1", "3.0", "2.1", "2.0", "1.0".
	SMBVersion string `json:"smb_version,omitempty"`
}

const (
	LocSMB   = "smb"
	LocLocal = "local"
)

// Location è una cartella: locale sul server oppure su una condivisione SMB.
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
		return "Mirror"
	case ModeMirrorArchive:
		return "Mirror + archivio"
	case ModeAdditive:
		return "Solo aggiunte"
	}
	return m
}

func ModeDescription(m string) string {
	switch m {
	case ModeMirror:
		return "La destinazione diventa identica alla sorgente: i file cancellati alla sorgente vengono cancellati."
	case ModeMirrorArchive:
		return "Come Mirror, ma i file cancellati o sovrascritti vengono spostati in " + ArchiveDirName + "/<data>."
	case ModeAdditive:
		return "Copia file nuovi e modificati, non cancella mai nulla nella destinazione."
	}
	return ""
}

// ArchiveDirName è la cartella (nella radice della destinazione) che contiene le versioni archiviate.
const ArchiveDirName = ".vegasyncor-archivio"

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
	// AllowEmptySource consente un mirror anche se la sorgente risulta vuota
	// (di norma viene bloccato per non svuotare la destinazione per errore).
	AllowEmptySource bool `json:"allow_empty_source,omitempty"`
	// BandwidthKBps limita la banda in KB/s (0 = illimitata).
	BandwidthKBps int `json:"bandwidth_kbps,omitempty"`
}

type Config struct {
	MaxParallel int          `json:"max_parallel"`
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

// Save scrive la configurazione in modo atomico (file temporaneo + rename).
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
		return errors.New("il nome della connessione è obbligatorio")
	}
	if cn.Host == "" || !hostRe.MatchString(cn.Host) {
		return errors.New("host non valido (es. 192.168.1.10 oppure server01)")
	}
	if strings.ContainsAny(cn.Username+cn.Domain, ",\n") {
		return errors.New("utente o dominio contengono caratteri non ammessi")
	}
	switch cn.SMBVersion {
	case "", "3.1.1", "3.0", "2.1", "2.0", "1.0":
	default:
		return errors.New("versione SMB non valida")
	}
	return nil
}

// CleanSubPath normalizza un percorso relativo all'interno di una condivisione.
func CleanSubPath(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("il percorso non può uscire dalla condivisione")
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
			return fmt.Errorf("%s: selezionare una connessione", what)
		}
		l.Share = strings.Trim(strings.TrimSpace(l.Share), `\/`)
		if l.Share == "" || strings.ContainsAny(l.Share, `/\,`) {
			return fmt.Errorf("%s: nome condivisione non valido", what)
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
			return fmt.Errorf("%s: il percorso locale deve essere assoluto", what)
		}
		if l.Path == "/" {
			return fmt.Errorf("%s: non è possibile usare la radice /", what)
		}
	default:
		return fmt.Errorf("%s: tipo sconosciuto", what)
	}
	return nil
}

// Display restituisce una rappresentazione leggibile della posizione.
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
		return errors.New("il nome del job è obbligatorio")
	}
	if err := j.Source.validate(c, "sorgente"); err != nil {
		return err
	}
	if err := j.Dest.validate(c, "destinazione"); err != nil {
		return err
	}
	if j.Source == j.Dest {
		return errors.New("sorgente e destinazione coincidono")
	}
	if j.Source.Type == LocLocal && j.Dest.Type == LocLocal {
		s, d := j.Source.Path+"/", j.Dest.Path+"/"
		if strings.HasPrefix(d, s) || strings.HasPrefix(s, d) {
			return errors.New("sorgente e destinazione non possono essere annidate")
		}
	}
	switch j.Mode {
	case ModeMirror, ModeAdditive:
	case ModeMirrorArchive:
		if j.ArchiveDays < 0 {
			return errors.New("giorni di archivio non validi")
		}
	default:
		return errors.New("modalità non valida")
	}
	if j.BandwidthKBps < 0 {
		return errors.New("limite di banda non valido")
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
