package mount

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"vegasyncor/internal/paths"
)

// Environment descrive dove gira VegaSyncor, per capire se può montare condivisioni SMB.
type Environment struct {
	// Container: "" se non è un container, altrimenti "lxc", "docker", "podman", ...
	Container string
	// Unprivileged: il processo gira in un user namespace (container non privilegiato),
	// dove il kernel non consente di montare filesystem CIFS.
	Unprivileged bool
	Root         bool
}

// file di sistema letti per il rilevamento (sostituibili nei test)
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
	// fuori da un user namespace l'unica riga è "0 0 4294967295"
	return !(len(f) == 3 && f[0] == "0" && f[1] == "0" && f[2] == "4294967295")
}

func detectContainer() string {
	c := rawContainer()
	if c == "wsl" {
		return "" // WSL2 è una VM leggera: i montaggi CIFS funzionano
	}
	return c
}

func rawContainer() string {
	// scritto da systemd all'avvio del container
	if raw, err := readFile("/run/systemd/container"); err == nil {
		if v := strings.TrimSpace(string(raw)); v != "" {
			return v
		}
	}
	// variabile d'ambiente del processo 1 (leggibile da root)
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

// Advice spiega come risolvere quando i montaggi SMB non sono consentiti.
func (e Environment) Advice() string {
	switch e.Container {
	case "lxc", "lxc-libvirt":
		return "usare un container privilegiato con la funzionalità SMB/CIFS attiva " +
			"(Proxmox: ripristinare il backup del container con --unprivileged 0, poi pct set <ID> --features mount=cifs) " +
			"oppure una macchina virtuale"
	case "docker", "podman":
		return "avviare il container con --privileged (oppure --cap-add SYS_ADMIN --cap-add DAC_READ_SEARCH) " +
			"o installare VegaSyncor direttamente sull'host"
	}
	return "usare un container privilegiato oppure una macchina virtuale"
}

// MountProblem restituisce una descrizione del motivo per cui l'ambiente non permette
// di montare condivisioni SMB, oppure "" se non ci sono impedimenti noti.
func (e Environment) MountProblem() string {
	if e.Unprivileged {
		name := "un container non privilegiato"
		if e.Container != "" {
			name = fmt.Sprintf("un container %s non privilegiato", strings.ToUpper(e.Container))
		}
		return "VegaSyncor gira in " + name + ": il kernel non consente di montare condivisioni di rete, " +
			"quindi le sincronizzazioni con cartelle SMB falliranno. Soluzione: " + e.Advice()
	}
	if !e.Root {
		return "il servizio non è in esecuzione come root: i montaggi falliranno"
	}
	return ""
}

// CheckLevel indica la gravità di un controllo.
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

// Diagnose esegue i controlli sull'ambiente di esecuzione.
func Diagnose() []Check {
	var out []Check
	env := DetectEnvironment()

	switch {
	case paths.DevMode():
		out = append(out, Check{CheckWarn, "Modalità sviluppo", "VEGASYNCOR_DEV=1: nessun montaggio reale"})
	case env.Root:
		out = append(out, Check{CheckOK, "Utente root", ""})
	default:
		out = append(out, Check{CheckFail, "Utente root", "eseguire come root (sudo); il servizio systemd gira già come root"})
	}

	switch {
	case env.Unprivileged:
		out = append(out, Check{CheckFail, "Montaggio condivisioni SMB", env.MountProblem()})
	case env.Container == "lxc":
		out = append(out, Check{CheckWarn, "Container LXC privilegiato",
			"verificare che sia attiva la funzionalità SMB/CIFS (Proxmox: pct set <ID> --features mount=cifs)"})
	case env.Container != "":
		out = append(out, Check{CheckWarn, "Container " + env.Container, "i montaggi SMB potrebbero non essere consentiti: " + env.Advice()})
	default:
		out = append(out, Check{CheckOK, "Ambiente", "macchina fisica o virtuale"})
	}

	for _, t := range []struct{ cmd, pkg string }{{"rsync", "rsync"}, {"mount.cifs", "cifs-utils"}} {
		if _, err := exec.LookPath(t.cmd); err != nil {
			out = append(out, Check{CheckFail, t.cmd, "non installato (apt install " + t.pkg + ")"})
		} else {
			out = append(out, Check{CheckOK, t.cmd, ""})
		}
	}
	if _, err := exec.LookPath("smbclient"); err != nil {
		out = append(out, Check{CheckWarn, "smbclient", "non installato: l'elenco delle condivisioni non sarà disponibile (apt install smbclient)"})
	} else {
		out = append(out, Check{CheckOK, "smbclient", ""})
	}
	return out
}
