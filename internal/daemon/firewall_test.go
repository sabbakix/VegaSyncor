package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vegasyncor/internal/config"
	"vegasyncor/internal/paths"
)

type fakeNFT struct {
	mu      sync.Mutex
	scripts []string
	fail    bool
}

func (f *fakeNFT) Apply(s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("nft: syntax error")
	}
	f.scripts = append(f.scripts, s)
	return nil
}
func (f *fakeNFT) List() ([]byte, error) { return nil, nil }
func (f *fakeNFT) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.scripts) == 0 {
		return ""
	}
	return f.scripts[len(f.scripts)-1]
}

func newTestDaemon(t *testing.T) (*Daemon, *fakeNFT) {
	dir := t.TempDir()
	t.Setenv("VEGASYNCOR_CONFIG_DIR", dir+"/etc")
	t.Setenv("VEGASYNCOR_STATE_DIR", dir+"/state")
	t.Setenv("VEGASYNCOR_RUNTIME_DIR", dir+"/run")
	t.Setenv("VEGASYNCOR_DEV", "1")
	t.Setenv("VEGASYNCOR_LANG", "en")
	d, err := New("test")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNFT{}
	d.fw.runner = fake
	return d, fake
}

func call(h http.HandlerFunc, method string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(method, "/", &b))
	return w
}

func TestFirewallRevertAndConfirm(t *testing.T) {
	old := ConfirmTimeout
	ConfirmTimeout = 150 * time.Millisecond
	defer func() { ConfirmTimeout = old }()
	d, fake := newTestDaemon(t)

	on := config.DefaultFirewall()
	on.Enabled = true
	on.AdminHosts = []string{"192.168.1.20"}

	// 1) not confirmed: applied, then reverted to the previous state (firewall off)
	if w := call(d.handleFirewallSet, "PUT", on); w.Code != 200 {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(fake.last(), "vs:in-ssh") {
		t.Fatalf("rules not applied:\n%s", fake.last())
	}
	if d.cfg.Firewall.Enabled {
		t.Fatal("unconfirmed settings saved")
	}
	time.Sleep(400 * time.Millisecond)
	if fake.last() != firewallDisable() {
		t.Fatalf("not reverted, last script:\n%s", fake.last())
	}

	// 2) confirmed: saved in the configuration file
	call(d.handleFirewallSet, "PUT", on)
	if w := call(d.handleFirewallConfirm, "POST", nil); w.Code != 200 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	time.Sleep(300 * time.Millisecond) // the timer must not fire anymore
	if strings.Contains(fake.last(), "delete table inet vegasyncor\n") && !strings.Contains(fake.last(), "vs:in-ssh") {
		t.Fatal("confirmed rules were reverted")
	}
	saved, _ := config.Load(paths.ConfigFile())
	if !saved.Firewall.Enabled || saved.Firewall.AdminHosts[0] != "192.168.1.20" {
		t.Fatalf("confirmed settings not saved: %+v", saved.Firewall)
	}

	// 3) a later change that is not confirmed returns to the confirmed rules
	changed := on
	changed.AdminHosts = []string{"10.9.9.9"}
	call(d.handleFirewallSet, "PUT", changed)
	time.Sleep(400 * time.Millisecond)
	if !strings.Contains(fake.last(), "192.168.1.20") || strings.Contains(fake.last(), "10.9.9.9") {
		t.Fatalf("not reverted to the confirmed rules:\n%s", fake.last())
	}

	// 4) nft rejects the rules: error, nothing pending
	fake.fail = true
	if w := call(d.handleFirewallSet, "PUT", changed); w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if d.fw.pending != nil {
		t.Fatal("pending change after a failed apply")
	}
	if len(d.warnings()) == 0 {
		t.Error("apply error not reported in the warnings")
	}

	// 5) turning it off is applied and saved at once
	fake.fail = false
	off := on
	off.Enabled = false
	if w := call(d.handleFirewallSet, "PUT", off); w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if saved, _ := config.Load(paths.ConfigFile()); saved.Firewall.Enabled {
		t.Error("disabled firewall not saved")
	}
}

func firewallDisable() string { return "table inet vegasyncor\ndelete table inet vegasyncor\n" }
