package config

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// Firewall holds the settings of the firewall managed by VegaSyncor
// (its own nftables table, see package firewall).
type Firewall struct {
	// Enabled applies the lockdown rules: incoming only SSH from AdminHosts,
	// outgoing only SMB to the connection hosts plus the basic services.
	Enabled bool `json:"enabled"`
	// SSHPort is the port of the SSH server (0 = 22).
	SSHPort int `json:"ssh_port,omitempty"`
	// AdminHosts are the IP addresses or networks (CIDR) allowed to connect over
	// SSH; empty = any host.
	AdminHosts []string `json:"admin_hosts,omitempty"`
	// AllowWeb allows outgoing HTTP/HTTPS (system updates, VegaSyncor updates).
	AllowWeb bool `json:"allow_web"`
	// AllowPing answers incoming ping requests.
	AllowPing bool `json:"allow_ping"`
	// Rules are additional allow/block rules.
	Rules []FwRule `json:"rules,omitempty"`
}

const (
	FwIn    = "in"
	FwOut   = "out"
	FwAllow = "allow"
	FwBlock = "block"
	FwTCP   = "tcp"
	FwUDP   = "udp"
	FwAny   = "any"
)

// FwRule allows or blocks traffic to/from a host or network on a port.
type FwRule struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`      // FwIn | FwOut
	Action    string `json:"action"`         // FwAllow | FwBlock
	Proto     string `json:"proto"`          // FwTCP | FwUDP | FwAny
	Port      string `json:"port,omitempty"` // "", "445" or "8000-8100" (local port for in, remote port for out)
	Host      string `json:"host,omitempty"` // "", IP or CIDR (remote host)
	Comment   string `json:"comment,omitempty"`
}

// DefaultFirewall is the lockdown preset proposed when the firewall is first enabled.
func DefaultFirewall() Firewall {
	return Firewall{SSHPort: 22, AllowWeb: true, AllowPing: true}
}

func (f *Firewall) SSH() int {
	if f.SSHPort <= 0 {
		return 22
	}
	return f.SSHPort
}

// ParseHostSpec validates an IP address or a network in CIDR notation and
// returns it in canonical form.
func ParseHostSpec(s string) (string, error) {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	if _, n, err := net.ParseCIDR(s); err == nil {
		return n.String(), nil
	}
	return "", errors.New(Tf("invalid address %q: use an IP address (e.g. 192.168.1.20) or a network (e.g. 192.168.1.0/24)", s))
}

// IsIPv6Spec reports whether a canonical host spec is an IPv6 address or network.
func IsIPv6Spec(s string) bool { return strings.Contains(s, ":") }

func parsePort(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	lo, hi, isRange := strings.Cut(s, "-")
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b := a
	var err2 error
	if isRange {
		b, err2 = strconv.Atoi(strings.TrimSpace(hi))
	}
	if err1 != nil || err2 != nil || a < 1 || b > 65535 || a > b {
		return "", errors.New(Tf("invalid port %q: use a number (e.g. 445) or a range (e.g. 8000-8100)", s))
	}
	if isRange {
		return strconv.Itoa(a) + "-" + strconv.Itoa(b), nil
	}
	return strconv.Itoa(a), nil
}

// Validate normalises and checks the firewall settings.
func (f *Firewall) Validate() error {
	if f.SSHPort < 0 || f.SSHPort > 65535 {
		return errors.New(T("invalid SSH port"))
	}
	var hosts []string
	for _, h := range f.AdminHosts {
		if strings.TrimSpace(h) == "" {
			continue
		}
		c, err := ParseHostSpec(h)
		if err != nil {
			return err
		}
		hosts = append(hosts, c)
	}
	f.AdminHosts = hosts
	for i := range f.Rules {
		if err := f.Rules[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r *FwRule) Validate() error {
	if r.ID == "" {
		r.ID = NewID()
	}
	switch r.Direction {
	case FwIn, FwOut:
	default:
		return errors.New(T("invalid rule direction"))
	}
	switch r.Action {
	case FwAllow, FwBlock:
	default:
		return errors.New(T("invalid rule action"))
	}
	switch r.Proto {
	case FwTCP, FwUDP, FwAny:
	case "":
		r.Proto = FwAny
	default:
		return errors.New(T("invalid protocol"))
	}
	p, err := parsePort(r.Port)
	if err != nil {
		return err
	}
	r.Port = p
	if strings.TrimSpace(r.Host) != "" {
		h, err := ParseHostSpec(r.Host)
		if err != nil {
			return err
		}
		r.Host = h
	} else {
		r.Host = ""
	}
	if r.Host == "" && r.Port == "" {
		return errors.New(T("a rule needs a host, a port or both"))
	}
	r.Comment = strings.Map(func(c rune) rune {
		if c < 32 || c == '"' || c == '\\' {
			return -1
		}
		return c
	}, strings.TrimSpace(r.Comment))
	if len(r.Comment) > 100 {
		r.Comment = r.Comment[:100]
	}
	return nil
}
