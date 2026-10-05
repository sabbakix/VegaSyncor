// Package mount handles mounting SMB/CIFS shares and read-only
// bind mounts of local sources.
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
	"regexp"
	"sort"
	"strings"
	"time"

	"vegasyncor/internal/paths"
)

// SMBTarget holds what is needed to mount a share.
type SMBTarget struct {
	Host       string
	Share      string
	Username   string
	Password   string
	Domain     string
	SMBVersion string
}

// Mount is an active mount point.
type Mount struct {
	Point string
}

// writeCredentials creates a temporary credentials file (0600) in RuntimeDir,
// so the password never appears on a process command line.
func writeCredentials(t SMBTarget) (string, error) {
	if strings.ContainsAny(t.Password, "\n\r") {
		return "", errors.New(T("the password cannot contain line breaks"))
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
		msg := cleanOutput(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s: %s", name, explainMountError(msg))
	}
	return nil
}

// cleanOutput reduces the mount output to one line, without the manual references.
func cleanOutput(out string) string {
	var parts []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "Refer to the mount.cifs(8)") {
			continue
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " ")
}

func uptime() float64 {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var v float64
	fmt.Sscanf(string(raw), "%f", &v)
	return v
}

var dmesgRe = regexp.MustCompile(`^\[\s*([0-9]+\.[0-9]+)\]\s*(.*)$`)

// kernelCIFSMessages returns the kernel CIFS messages logged after since
// (seconds since boot): they explain the real reason of a failed mount.
func kernelCIFSMessages(since float64) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "dmesg")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseCIFSMessages(string(out), since)
}

func parseCIFSMessages(out string, since float64) string {
	var msgs []string
	for _, l := range strings.Split(out, "\n") {
		m := dmesgRe.FindStringSubmatch(l)
		if m == nil || !strings.Contains(m[2], "CIFS") {
			continue
		}
		var ts float64
		fmt.Sscanf(m[1], "%f", &ts)
		if ts >= since-1 {
			msgs = append(msgs, m[2])
		}
	}
	if len(msgs) > 3 {
		msgs = msgs[len(msgs)-3:]
	}
	return strings.Join(msgs, "; ")
}

// explainMountError adds a hint to the most common mount.cifs errors.
func explainMountError(msg string) string {
	hints := map[string]string{
		"Permission denied":         T("wrong user or password, or insufficient permissions on the share"),
		"Operation not permitted":   epermHint(),
		"No such file or directory": T("share does not exist or wrong path"),
		"Host is down":              T("server unreachable or SMB version not supported (try setting it)"),
		"could not resolve address": T("host name cannot be resolved: try the IP address"),
		"Operation not supported":   T("SMB version not supported by the server: set it explicitly"),
		"No route to host":          T("server unreachable on the network"),
		"Connection refused":        T("the server does not accept SMB connections (port 445)"),
		"Operation now in progress": T("server unreachable (timeout)"),
		"wrong fs type":             T("the cifs-utils package is missing (apt install cifs-utils)"),
	}
	for k, h := range hints {
		if h != "" && strings.Contains(msg, k) {
			return msg + " → " + h
		}
	}
	return msg
}

// epermHint explains "Operation not permitted" based on where the service runs.
func epermHint() string {
	env := DetectEnvironment()
	switch {
	case env.Unprivileged:
		return env.Advice()
	case env.Container == "lxc":
		return T("privileged LXC container: enable the container's SMB/CIFS feature (Proxmox: pct set <ID> --features mount=cifs, then restart it); if already enabled, check user, password and domain")
	case env.Container != "":
		return Tf("the service runs in a container (%s) that may not allow mounts: %s", env.Container, env.Advice())
	case os.Geteuid() != 0:
		return T("the service is not running as root (start it with systemctl start vegasyncor)")
	}
	return T("the server refused access: check user, password and domain (leave the domain empty for a local Windows user) and try setting the SMB version")
}

// devSMB simulates a share with a local folder (development mode only).
func devSMB(t SMBTarget) (*Mount, error) {
	root := paths.DevSMBRoot()
	if root == "" {
		return nil, errors.New(T("development mode: set VEGASYNCOR_DEV_SMB_ROOT to simulate SMB shares"))
	}
	p := filepath.Join(root, t.Host, t.Share)
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("mount: //%s/%s: No such file or directory → %s", t.Host, t.Share, T("share does not exist or wrong path"))
	}
	return &Mount{Point: p}, nil
}

// EnsureTools checks that the required commands are installed.
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
		return errors.New(Tf("missing commands: %s (apt install cifs-utils rsync)", strings.Join(missing, ", ")))
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
		return "", errors.New(Tf("%s is already mounted", p))
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return p, nil
}

// MountSMB mounts //host/share on a dedicated mount point.
// readOnly=true uses the "ro" option: the kernel prevents any write.
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
	since := uptime()
	if err := run(ctx, "mount", "-t", "cifs", unc, point, "-o", strings.Join(opts, ",")); err != nil {
		os.Remove(point)
		if k := kernelCIFSMessages(since); k != "" {
			err = fmt.Errorf("%w [kernel: %s]", err, k)
		}
		return nil, err
	}
	return &Mount{Point: point}, nil
}

// BindReadOnly exposes a local folder read-only through a bind mount.
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

// Unmount unmounts and removes the mount point, falling back to a lazy unmount.
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

// mounts returns the active mount points (from /proc/self/mounts).
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

// CleanupStale unmounts leftovers of previous runs (e.g. after a crash).
func CleanupStale() []string {
	base := paths.MountDir() + "/"
	var stale []string
	for _, m := range mounts() {
		if strings.HasPrefix(m, base) {
			stale = append(stale, m)
		}
	}
	// unmount the deepest paths first
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

// ListShares lists the disk shares of a server using smbclient (if installed).
func ListShares(ctx context.Context, t SMBTarget) ([]string, error) {
	if paths.DevMode() && paths.DevSMBRoot() != "" {
		entries, err := os.ReadDir(filepath.Join(paths.DevSMBRoot(), t.Host))
		if err != nil {
			return nil, fmt.Errorf("smbclient: %s: %s", t.Host, T("server unreachable"))
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
		return nil, errors.New(T("smbclient is not installed (apt install smbclient): enter the share name manually"))
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
