// Package paths centralizza le posizioni dei file usati da VegaSyncor.
// Ogni percorso può essere ridefinito tramite variabile d'ambiente
// (utile per test e sviluppo).
package paths

import (
	"os"
	"path/filepath"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// ConfigDir contiene config.json e master.key.
func ConfigDir() string { return env("VEGASYNCOR_CONFIG_DIR", "/etc/vegasyncor") }

// StateDir contiene storico e log delle esecuzioni.
func StateDir() string { return env("VEGASYNCOR_STATE_DIR", "/var/lib/vegasyncor") }

// RuntimeDir contiene socket, punti di montaggio e file temporanei.
func RuntimeDir() string { return env("VEGASYNCOR_RUNTIME_DIR", "/run/vegasyncor") }

func ConfigFile() string  { return filepath.Join(ConfigDir(), "config.json") }
func KeyFile() string     { return filepath.Join(ConfigDir(), "master.key") }
func HistoryFile() string { return filepath.Join(StateDir(), "history.json") }
func LogDir() string      { return filepath.Join(StateDir(), "logs") }
func MountDir() string    { return filepath.Join(RuntimeDir(), "mnt") }
func Socket() string      { return env("VEGASYNCOR_SOCKET", filepath.Join(RuntimeDir(), "vegasyncor.sock")) }

// DevMode disattiva i montaggi reali (solo per sviluppo senza root):
// le sorgenti locali vengono lette direttamente e le condivisioni SMB sono
// simulate con cartelle locali (vedi DevSMBRoot).
func DevMode() bool { return os.Getenv("VEGASYNCOR_DEV") == "1" }

// DevSMBRoot, in modalità sviluppo, è la cartella che simula i server SMB:
// \\HOST\SHARE corrisponde a <DevSMBRoot>/HOST/SHARE.
func DevSMBRoot() string { return os.Getenv("VEGASYNCOR_DEV_SMB_ROOT") }
