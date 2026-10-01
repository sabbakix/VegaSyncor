// Package mount gestisce il montaggio delle condivisioni SMB/CIFS
// e i bind mount in sola lettura delle sorgenti locali.
package mount

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vegasyncor/internal/paths"
)

// SMBTarget sono i dati necessari per montare una condivisione.
type SMBTarget struct {
	Host       string
	Share      string
	Username   string
	Password   string
	Domain     string
	SMBVersion string
}

// Mount rappresenta un punto di montaggio attivo.
type Mount struct {
	Point string
}

// writeCredentials crea un file credenziali temporaneo (0600) in RuntimeDir,
// così la password non compare mai nella riga di comando dei processi.
func writeCredentials(t SMBTarget) (string, error) {
	if strings.ContainsAny(t.Password, "\n\r") {
		return "", errors.New("la password non può contenere a capo")
	}
	dir := paths.RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".cred-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "username=%s\npassword=%s\n", t.Username, t.Password)
	if t.Domain != "" {
		fmt.Fprintf(&b, "domain=%s\n", t.Domain)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func run(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s", name, explainMountError(msg))
	}
	return nil
}

// explainMountError aggiunge un suggerimento in italiano agli errori più comuni di mount.cifs.
func explainMountError(msg string) string {
	hints := map[string]string{
		"Permission denied":         "utente o password errati, oppure permessi insufficienti sulla condivisione",
		"No such file or directory": "condivisione inesistente o percorso errato",
		"Host is down":              "server non raggiungibile oppure versione SMB non supportata (provare a impostarla)",
		"could not resolve address": "nome host non risolvibile: provare con l'indirizzo IP",
		"Operation not supported":   "versione SMB non supportata dal server: impostarla esplicitamente",
		"No route to host":          "server non raggiungibile in rete",
		"Connection refused":        "il server non accetta connessioni SMB (porta 445)",
		"Operation now in progress": "server non raggiungibile (timeout)",
		"cannot mount":              "",
		"wrong fs type":             "manca il pacchetto cifs-utils (apt install cifs-utils)",
	}
	for k, h := range hints {
		if h != "" && strings.Contains(msg, k) {
			return msg + " → " + h
		}
	}
	return msg
}

// devSMB simula una condivisione con una cartella locale (solo modalità sviluppo).
func devSMB(t SMBTarget) (*Mount, error) {
	root := paths.DevSMBRoot()
	if root == "" {
		return nil, errors.New("modalità sviluppo: impostare VEGASYNCOR_DEV_SMB_ROOT per simulare le condivisioni SMB")
	}
	p := filepath.Join(root, t.Host, t.Share)
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("mount: //%s/%s: No such file or directory → condivisione inesistente o percorso errato", t.Host, t.Share)
	}
	return &Mount{Point: p}, nil
}

// EnsureTools verifica che i comandi necessari siano installati.
func EnsureTools() error {
	tools := []string{"mount.cifs", "rsync", "mount", "umount"}
	if paths.DevMode() {
		tools = []string{"rsync"}
	}
	var missing []string
	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("comandi mancanti: %s (apt install cifs-utils rsync)", strings.Join(missing, ", "))
	}
	return nil
}

func newPoint(name string) (string, error) {
	base := paths.MountDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(base, name)
	if IsMounted(p) {
		return "", fmt.Errorf("%s risulta già montato", p)
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

// MountSMB monta //host/share su un punto di montaggio dedicato.
// readOnly=true usa l'opzione "ro": il kernel impedisce qualsiasi scrittura.
func MountSMB(ctx context.Context, name string, t SMBTarget, readOnly bool) (*Mount, error) {
	if paths.DevMode() {
		return devSMB(t)
	}
	point, err := newPoint(name)
	if err != nil {
		return nil, err
	}
	cred, err := writeCredentials(t)
	if err != nil {
		return nil, err
	}
	defer os.Remove(cred)

	opts := []string{"credentials=" + cred, "iocharset=utf8", "noserverino", "uid=0", "gid=0", "nounix", "soft"}
	if readOnly {
		opts = append([]string{"ro"}, opts...)
		opts = append(opts, "file_mode=0444", "dir_mode=0555")
	} else {
		opts = append([]string{"rw"}, opts...)
		opts = append(opts, "file_mode=0660", "dir_mode=0770")
	}
	if t.SMBVersion != "" {
		opts = append(opts, "vers="+t.SMBVersion)
	}
	unc := "//" + t.Host + "/" + t.Share
	if err := run(ctx, "mount", "-t", "cifs", unc, point, "-o", strings.Join(opts, ",")); err != nil {
		os.Remove(point)
		return nil, err
	}
	return &Mount{Point: point}, nil
}

// BindReadOnly espone una cartella locale in sola lettura tramite bind mount.
func BindReadOnly(ctx context.Context, name, src string) (*Mount, error) {
	if paths.DevMode() {
		return &Mount{Point: src}, nil
	}
	point, err := newPoint(name)
	if err != nil {
		return nil, err
	}
	if err := run(ctx, "mount", "--bind", src, point); err != nil {
		os.Remove(point)
		return nil, err
	}
	if err := run(ctx, "mount", "-o", "remount,bind,ro", point); err != nil {
		_ = run(context.Background(), "umount", point)
		os.Remove(point)
		return nil, err
	}
	return &Mount{Point: point}, nil
}

// Unmount smonta e rimuove il punto di montaggio. Usa il lazy unmount come ripiego.
func (m *Mount) Unmount() error {
	if m == nil || paths.DevMode() && !strings.HasPrefix(m.Point, paths.MountDir()) {
		return nil
	}
	err := run(context.Background(), "umount", m.Point)
	if err != nil {
		if err2 := run(context.Background(), "umount", "-l", m.Point); err2 != nil {
			return err
		}
	}
	return os.Remove(m.Point)
}

// mounts restituisce i punti di montaggio attivi (da /proc/self/mounts).
func mounts() []string {
	raw, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 {
			out = append(out, unescape(f[1]))
		}
	}
	return out
}

func unescape(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func IsMounted(p string) bool {
	for _, m := range mounts() {
		if m == p {
			return true
		}
	}
	return false
}

// CleanupStale smonta eventuali residui di esecuzioni precedenti (es. dopo un crash).
func CleanupStale() []string {
	base := paths.MountDir() + "/"
	var stale []string
	for _, m := range mounts() {
		if strings.HasPrefix(m, base) {
			stale = append(stale, m)
		}
	}
	// smonta prima i percorsi più profondi
	sort.Sort(sort.Reverse(sort.StringSlice(stale)))
	for _, p := range stale {
		_ = (&Mount{Point: p}).Unmount()
	}
	if entries, err := os.ReadDir(paths.MountDir()); err == nil {
		for _, e := range entries {
			os.Remove(filepath.Join(paths.MountDir(), e.Name()))
		}
	}
	return stale
}

// ListShares elenca le condivisioni disco di un server usando smbclient (se installato).
func ListShares(ctx context.Context, t SMBTarget) ([]string, error) {
	if paths.DevMode() && paths.DevSMBRoot() != "" {
		entries, err := os.ReadDir(filepath.Join(paths.DevSMBRoot(), t.Host))
		if err != nil {
			return nil, fmt.Errorf("smbclient: %s: server non raggiungibile", t.Host)
		}
		var shares []string
		for _, e := range entries {
			if e.IsDir() {
				shares = append(shares, e.Name())
			}
		}
		return shares, nil
	}
	if _, err := exec.LookPath("smbclient"); err != nil {
		return nil, errors.New("smbclient non installato (apt install smbclient): inserire il nome della condivisione a mano")
	}
	dir := paths.RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".auth-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	f.Chmod(0o600)
	fmt.Fprintf(f, "username = %s\npassword = %s\n", t.Username, t.Password)
	if t.Domain != "" {
		fmt.Fprintf(f, "domain = %s\n", t.Domain)
	}
	f.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"-L", "//" + t.Host, "-A", f.Name(), "-g"}
	cmd := exec.CommandContext(ctx, "smbclient", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	var shares []string
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.Split(line, "|")
		if len(p) >= 2 && p[0] == "Disk" && !strings.HasSuffix(p[1], "$") {
			shares = append(shares, p[1])
		}
	}
	if err != nil && len(shares) == 0 {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return nil, fmt.Errorf("smbclient: %s", explainMountError(msg))
	}
	sort.Strings(shares)
	return shares, nil
}
