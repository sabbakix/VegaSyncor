package mount

import (
	"os"
	"strings"
	"testing"
)

func fakeFS(t *testing.T, files map[string]string) {
	t.Helper()
	old := readFile
	readFile = func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { readFile = old })
}

func TestDetectEnvironment(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		container   string
		unpriv      bool
		problemHint string
	}{
		{"macchina fisica", map[string]string{"/proc/self/uid_map": "         0          0 4294967295\n"}, "", false, ""},
		{"LXC non privilegiato", map[string]string{
			"/proc/self/uid_map": "         0     100000      65536\n", "/run/systemd/container": "lxc\n"},
			"lxc", true, "pct set <ID> --features mount=cifs"},
		{"LXC privilegiato", map[string]string{
			"/proc/self/uid_map": "0 0 4294967295", "/proc/1/environ": "PATH=/bin\x00container=lxc\x00"}, "lxc", false, ""},
		{"Docker", map[string]string{"/proc/self/uid_map": "0 0 4294967295", "/.dockerenv": ""}, "docker", false, ""},
		{"WSL", map[string]string{"/proc/self/uid_map": "0 0 4294967295", "/run/systemd/container": "wsl"}, "", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeFS(t, c.files)
			e := DetectEnvironment()
			e.Root = true
			if e.Container != c.container || e.Unprivileged != c.unpriv {
				t.Fatalf("got %+v", e)
			}
			p := e.MountProblem()
			if (p != "") != c.unpriv || !strings.Contains(p, c.problemHint) {
				t.Errorf("MountProblem = %q", p)
			}
		})
	}
}
