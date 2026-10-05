package mount

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"vegasyncor/internal/paths"
)

// Environment describes where VegaSyncor runs, to tell whether it can mount SMB shares.
type Environment struct {
	// Container: "" if not a container, otherwise "lxc", "docker", "podman", ...
	Container string
	// Unprivileged: the process runs in a user namespace (unprivileged container),
	// where the kernel does not allow mounting CIFS filesystems.
	Unprivileged bool
	Root         bool
}

// system files read for detection (replaceable in tests)
var readFile = os.ReadFile

func DetectEnvironment() Environment {
	return Environment{
		Container:    detectContainer(),
		Unprivileged: inUserNamespace(),
		Root:         os.Geteuid() == 0,
	}
}

func inUserNamespace() bool {
	raw, err := readFile("/proc/self/uid_map")
	if err != nil {
		return false
	}
	f := strings.Fields(string(raw))
	// outside a user namespace the only line is "0 0 4294967295"
	return !(len(f) == 3 && f[0] == "0" && f[1] == "0" && f[2] == "4294967295")
}

func detectContainer() string {
	c := rawContainer()
	if c == "wsl" {
		return "" // WSL2 is a lightweight VM: CIFS mounts work
	}
	return c
}

func rawContainer() string {
	// written by systemd when the container starts
	if raw, err := readFile("/run/systemd/container"); err == nil {
		if v := strings.TrimSpace(string(raw)); v != "" {
			return v
		}
	}
	// environment of process 1 (readable by root)
	if raw, err := readFile("/proc/1/environ"); err == nil {
		for _, kv := range bytes.Split(raw, []byte{0}) {
			if v, ok := strings.CutPrefix(string(kv), "container="); ok && v != "" {
				return v
			}
		}
	}
	if _, err := readFile("/.dockerenv"); err == nil {
		return "docker"
	}
	return ""
}

// Advice explains how to fix it when SMB mounts are not allowed.
func (e Environment) Advice() string {
	switch e.Container {
	case "lxc", "lxc-libvirt":
		return T("use a privileged container with the SMB/CIFS feature enabled " +
			"(Proxmox: restore the container backup with --unprivileged 0, then pct set <ID> --features mount=cifs) " +
			"or a virtual machine")
	case "docker", "podman":
		return T("start the container with --privileged (or --cap-add SYS_ADMIN --cap-add DAC_READ_SEARCH) " +
			"or install VegaSyncor directly on the host")
	}
	return T("use a privileged container or a virtual machine")
}

// MountProblem describes why the environment does not allow mounting SMB shares,
// or returns "" if there are no known obstacles.
func (e Environment) MountProblem() string {
	if e.Unprivileged {
		name := T("an unprivileged container")
		if e.Container != "" {
			name = Tf("an unprivileged %s container", strings.ToUpper(e.Container))
		}
		return Tf("VegaSyncor runs in %s: the kernel does not allow mounting network shares, "+
			"so syncs with SMB folders will fail. Solution: %s", name, e.Advice())
	}
	if !e.Root {
		return T("the service is not running as root: mounts will fail")
	}
	return ""
}

// CheckLevel is the severity of a check.
type CheckLevel int

const (
	CheckOK CheckLevel = iota
	CheckWarn
	CheckFail
)

type Check struct {
	Level  CheckLevel
	Name   string
	Detail string
}

// Diagnose runs the checks on the runtime environment.
func Diagnose() []Check {
	var out []Check
	env := DetectEnvironment()

	switch {
	case paths.DevMode():
		out = append(out, Check{CheckWarn, T("Development mode"), T("VEGASYNCOR_DEV=1: no real mounts")})
	case env.Root:
		out = append(out, Check{CheckOK, T("Root user"), ""})
	default:
		out = append(out, Check{CheckFail, T("Root user"), T("run as root (sudo); the systemd service already runs as root")})
	}

	switch {
	case env.Unprivileged:
		out = append(out, Check{CheckFail, T("Mounting SMB shares"), env.MountProblem()})
	case env.Container == "lxc":
		out = append(out, Check{CheckWarn, T("Privileged LXC container"),
			T("make sure the SMB/CIFS feature is enabled (Proxmox: pct set <ID> --features mount=cifs)")})
	case env.Container != "":
		out = append(out, Check{CheckWarn, "Container " + env.Container, T("SMB mounts may not be allowed:") + " " + env.Advice()})
	default:
		out = append(out, Check{CheckOK, T("Environment"), T("physical or virtual machine")})
	}

	for _, t := range []struct{ cmd, pkg string }{{"rsync", "rsync"}, {"mount.cifs", "cifs-utils"}} {
		if _, err := exec.LookPath(t.cmd); err != nil {
			out = append(out, Check{CheckFail, t.cmd, Tf("not installed (apt install %s)", t.pkg)})
		} else {
			out = append(out, Check{CheckOK, t.cmd, ""})
		}
	}
	if _, err := exec.LookPath("smbclient"); err != nil {
		out = append(out, Check{CheckWarn, "smbclient", T("not installed: the list of shares will not be available (apt install smbclient)")})
	} else {
		out = append(out, Check{CheckOK, "smbclient", ""})
	}
	return out
}
