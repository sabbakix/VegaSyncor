package transfer

import (
	"errors"
	"strings"
	"testing"

	"vegasyncor/internal/config"
	"vegasyncor/internal/secrets"
)

func box(t *testing.T) *secrets.Box {
	b, err := secrets.LoadOrCreate(t.TempDir() + "/master.key")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sample(t *testing.T, b *secrets.Box) *config.Config {
	enc, _ := b.Encrypt("S3cret, with ù", "c1")
	return &config.Config{
		MaxParallel: 3, Language: "it",
		Connections: []config.Connection{{ID: "c1", Name: "PC", Host: "192.168.1.20", Username: "backup", PasswordEnc: enc}},
		Jobs: []config.Job{{ID: "j1", Name: "Accounting", Enabled: true,
			Source: config.Location{Type: "smb", ConnectionID: "c1", Share: "Docs"}, SourceRO: true,
			Dest: config.Location{Type: "local", Path: "/srv/backup"}, Mode: config.ModeMirror,
			Schedule: config.Schedule{Type: config.SchedManual}}},
		Firewall: config.Firewall{Enabled: true, SSHPort: 22, AdminHosts: []string{"192.168.1.5"},
			Rules: []config.FwRule{{ID: "r1", Direction: "in", Action: "allow", Proto: "tcp", Port: "9100"}}},
	}
}

func TestRoundTripToNewServer(t *testing.T) {
	oldBox, newBox := box(t), box(t) // two servers, two different master keys
	cfg := sample(t, oldBox)

	p, err := BuildPayload(cfg, oldBox)
	if err != nil {
		t.Fatal(err)
	}
	file, err := Encrypt(p, "correct horse battery", "old-server", "test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "S3cret") || strings.Contains(string(file), "Accounting") {
		t.Fatal("clear text found in the export file")
	}

	got, h, err := Decrypt(file, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if h.Host != "old-server" {
		t.Errorf("header host = %q", h.Host)
	}
	next, res, err := BuildConfig(got, &config.Config{}, newBox)
	if err != nil {
		t.Fatal(err)
	}
	if res.Connections != 1 || res.Jobs != 1 || !res.FirewallImported || res.FirewallKept {
		t.Errorf("result = %+v", res)
	}
	// the password is now encrypted with the key of the new server
	pw, err := newBox.Decrypt(next.Connections[0].PasswordEnc, "c1")
	if err != nil || pw != "S3cret, with ù" {
		t.Fatalf("password after import = %q, %v", pw, err)
	}
	if _, err := oldBox.Decrypt(next.Connections[0].PasswordEnc, "c1"); err == nil {
		t.Error("imported password still readable with the old master key")
	}
	// the firewall is imported but never enabled automatically
	if next.Firewall.Enabled || len(next.Firewall.Rules) != 1 {
		t.Errorf("firewall after import = %+v", next.Firewall)
	}
	if next.MaxParallel != 3 || next.Language != "it" || next.Jobs[0].Name != "Accounting" {
		t.Errorf("config after import = %+v", next)
	}
}

func TestWrongPasswordAndTampering(t *testing.T) {
	b := box(t)
	p, _ := BuildPayload(sample(t, b), b)
	file, _ := Encrypt(p, "correct horse battery", "h", "v")
	if _, _, err := Decrypt(file, "wrong password!"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong password: err = %v", err)
	}
	// altering the clear-text header (e.g. the host) must be detected
	tampered := strings.Replace(string(file), `"host": "h"`, `"host": "x"`, 1)
	if _, _, err := Decrypt([]byte(tampered), "correct horse battery"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("tampered header accepted: %v", err)
	}
	if _, _, err := Decrypt([]byte(`{"hello":1}`), "x"); err == nil {
		t.Error("random JSON accepted")
	}
	if _, err := Encrypt(p, "short", "h", "v"); err == nil {
		t.Error("short passphrase accepted")
	}
}

func TestFirewallKeptWhenActive(t *testing.T) {
	b := box(t)
	p, _ := BuildPayload(sample(t, b), b)
	current := &config.Config{Firewall: config.Firewall{Enabled: true, SSHPort: 2222, AdminHosts: []string{"10.0.0.9"}}}
	next, res, err := BuildConfig(&p, current, b)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FirewallKept || !next.Firewall.Enabled || next.Firewall.SSHPort != 2222 {
		t.Errorf("active firewall of this server not kept: %+v %+v", res, next.Firewall)
	}
}
