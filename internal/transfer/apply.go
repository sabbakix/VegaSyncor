package transfer

import (
	"errors"
	"fmt"

	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
	"vegasyncor/internal/secrets"
)

// BuildPayload prepares the content of an export: the configuration with the
// connection passwords decrypted with the master key of this server.
func BuildPayload(cfg *config.Config, box *secrets.Box) (Payload, error) {
	p := Payload{Config: *cfg, Passwords: map[string]string{}}
	p.Config.Connections = append([]config.Connection(nil), cfg.Connections...)
	for i := range p.Config.Connections {
		c := &p.Config.Connections[i]
		pw, err := box.Decrypt(c.PasswordEnc, c.ID)
		if err != nil {
			return p, fmt.Errorf("%s %s: %w", T("connection"), c.Name, err)
		}
		if pw != "" {
			p.Passwords[c.ID] = pw
		}
		c.PasswordEnc = ""
	}
	return p, nil
}

// Result describes an import.
type Result struct {
	Connections int `json:"connections"`
	Jobs        int `json:"jobs"`
	// FirewallKept: the firewall of this server was active and its settings were kept.
	FirewallKept bool `json:"firewall_kept"`
	// FirewallImported: firewall settings were imported but left disabled.
	FirewallImported bool `json:"firewall_imported"`
}

// BuildConfig turns an imported payload into the configuration of this server:
// passwords are encrypted with the local master key and everything is validated.
// The imported firewall is never enabled automatically (the admin hosts and the
// network of the new server may differ): if the firewall of this server is active
// its settings are kept, otherwise the imported ones are stored disabled.
func BuildConfig(p *Payload, current *config.Config, box *secrets.Box) (*config.Config, Result, error) {
	var res Result
	next := p.Config
	if next.MaxParallel < 1 {
		next.MaxParallel = 1
	}
	if !i18n.Supported(next.Language) {
		next.Language = current.Language
	}
	next.Connections = nil
	for _, c := range p.Config.Connections {
		if err := c.Validate(); err != nil {
			return nil, res, fmt.Errorf("%s %q: %w", T("connection"), c.Name, err)
		}
		c.PasswordEnc = ""
		if pw := p.Passwords[c.ID]; pw != "" {
			enc, err := box.Encrypt(pw, c.ID)
			if err != nil {
				return nil, res, err
			}
			c.PasswordEnc = enc
		}
		next.Connections = append(next.Connections, c)
	}
	next.Jobs = nil
	names := map[string]bool{}
	for _, j := range p.Config.Jobs {
		if err := j.Validate(&next); err != nil {
			return nil, res, fmt.Errorf("%s %q: %w", T("job"), j.Name, err)
		}
		if names[j.Name] {
			return nil, res, errors.New(Tf("duplicate job name %q in the export file", j.Name))
		}
		names[j.Name] = true
		next.Jobs = append(next.Jobs, j)
	}
	if current.Firewall.Enabled {
		next.Firewall = current.Firewall
		res.FirewallKept = true
	} else {
		next.Firewall.Enabled = false
		if err := next.Firewall.Validate(); err != nil {
			return nil, res, err
		}
		res.FirewallImported = len(next.Firewall.Rules) > 0 || len(next.Firewall.AdminHosts) > 0
	}
	res.Connections, res.Jobs = len(next.Connections), len(next.Jobs)
	return &next, res, nil
}
