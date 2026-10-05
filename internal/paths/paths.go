// Package paths centralises the locations of the files used by VegaSyncor.
// Every path can be overridden with an environment variable
// (useful for tests and development).
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

// ConfigDir holds config.json and master.key.
func ConfigDir() string { return env("VEGASYNCOR_CONFIG_DIR", "/etc/vegasyncor") }

// StateDir holds the run history and logs.
func StateDir() string { return env("VEGASYNCOR_STATE_DIR", "/var/lib/vegasyncor") }

// RuntimeDir holds the socket, mount points and temporary files.
func RuntimeDir() string { return env("VEGASYNCOR_RUNTIME_DIR", "/run/vegasyncor") }

func ConfigFile() string  { return filepath.Join(ConfigDir(), "config.json") }
func KeyFile() string     { return filepath.Join(ConfigDir(), "master.key") }
func HistoryFile() string { return filepath.Join(StateDir(), "history.json") }
func LogDir() string      { return filepath.Join(StateDir(), "logs") }
func MountDir() string    { return filepath.Join(RuntimeDir(), "mnt") }
func Socket() string      { return env("VEGASYNCOR_SOCKET", filepath.Join(RuntimeDir(), "vegasyncor.sock")) }

// DevMode disables real mounts (development without root only):
// local sources are read directly and SMB shares are simulated
// with local folders (see DevSMBRoot).
func DevMode() bool { return os.Getenv("VEGASYNCOR_DEV") == "1" }

// DevSMBRoot, in development mode, is the folder simulating the SMB servers:
// \\HOST\SHARE maps to <DevSMBRoot>/HOST/SHARE.
func DevSMBRoot() string { return os.Getenv("VEGASYNCOR_DEV_SMB_ROOT") }
