package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/firewall"
)

// ConfirmTimeout is how long a firewall change stays active without confirmation
// before it is reverted (protection against locking oneself out over SSH).
var ConfirmTimeout = 60 * time.Second

// hostResolveEvery is how often the addresses of the connection hosts are refreshed.
const hostResolveEvery = 10 * time.Minute

type fwState struct {
	mu      sync.Mutex // serialises the nft calls and the fields below
	runner  firewall.Runner
	pending *fwPending
	lastErr string
	applied string // last script applied (to skip identical re-applies)

	hostsMu    sync.Mutex
	hosts      map[string][]string // connection host -> resolved IPs
	resolved   time.Time
	hostNames  map[string]string // IP -> connection label
	unresolved []string          // connection hosts without a known address
}

type fwPending struct {
	next     config.Firewall // applied, not yet saved
	prev     config.Firewall // confirmed settings, restored on timeout
	deadline time.Time
	timer    *time.Timer
}

func newFwState() *fwState {
	return &fwState{runner: firewall.NFT{}}
}

// fwAvailable reports whether rules can be applied (nft installed).
func (d *Daemon) fwAvailable() bool {
	_, isNFT := d.fw.runner.(firewall.NFT)
	return !isNFT || firewall.Available()
}

// --- addresses of the connection hosts ---

// smbHosts returns the IP addresses of the hosts used by the connections
// (resolving names), and refreshes the labels shown in the TUI.
func (d *Daemon) smbHosts(force bool) []string {
	d.mu.Lock()
	var hosts []string
	labels := map[string]string{}
	for _, c := range d.cfg.Connections {
		hosts = append(hosts, c.Host)
		if labels[c.Host] == "" {
			labels[c.Host] = c.Name
		}
	}
	d.mu.Unlock()

	fw := d.fw
	fw.hostsMu.Lock()
	defer fw.hostsMu.Unlock()
	if force || fw.hosts == nil || time.Since(fw.resolved) > hostResolveEvery {
		resolved := map[string][]string{}
		for _, h := range hosts {
			if ip := net.ParseIP(h); ip != nil {
				resolved[h] = []string{ip.String()}
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			addrs, err := net.DefaultResolver.LookupHost(ctx, h)
			cancel()
			if err != nil {
				slog.Warn("firewall: cannot resolve connection host", "host", h, "error", err)
				resolved[h] = fw.hosts[h] // keep the last known addresses
				continue
			}
			resolved[h] = addrs
		}
		fw.hosts, fw.resolved = resolved, time.Now()
		fw.unresolved = nil
		for _, h := range hosts {
			if len(resolved[h]) == 0 {
				fw.unresolved = append(fw.unresolved, h)
			}
		}
		sort.Strings(fw.unresolved)
	}
	names := map[string]string{}
	set := map[string]bool{}
	for h, ips := range fw.hosts {
		for _, ip := range ips {
			set[ip] = true
			label := h
			if labels[h] != "" && !strings.EqualFold(labels[h], h) {
				label = labels[h] + " (" + h + ")"
			}
			names[ip] = label
		}
	}
	fw.hostNames = names
	out := make([]string, 0, len(set))
	for ip := range set {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

// --- applying ---

func (d *Daemon) fwScript(f config.Firewall, forceResolve bool) string {
	if !f.Enabled {
		return firewall.DisableScript()
	}
	return firewall.Script(firewall.Plan(f, d.smbHosts(forceResolve)))
}

// fwApplyLocked loads a script; requires d.fw.mu.
func (d *Daemon) fwApplyLocked(script string) error {
	if !d.fwAvailable() {
		return errors.New(T("nftables is not installed (apt install nftables)"))
	}
	if err := d.fw.runner.Apply(script); err != nil {
		d.fw.lastErr = err.Error()
		return err
	}
	d.fw.lastErr = ""
	d.fw.applied = script
	return nil
}

// fwStartup applies the confirmed rules when the service starts.
func (d *Daemon) fwStartup() {
	d.mu.Lock()
	f := d.cfg.Firewall
	d.mu.Unlock()
	if !f.Enabled {
		return
	}
	d.fw.mu.Lock()
	defer d.fw.mu.Unlock()
	if err := d.fwApplyLocked(d.fwScript(f, true)); err != nil {
		slog.Error("firewall: cannot apply the rules", "error", err)
	} else {
		slog.Info("firewall: rules applied")
	}
}

// fwRefresh re-applies the confirmed rules when the addresses of the connection
// hosts may have changed (connections edited, periodic re-resolution). Only the
// outgoing SMB rule can change, so no confirmation is needed. Skipped while a
// change is pending.
func (d *Daemon) fwRefresh(forceResolve bool) {
	d.mu.Lock()
	f := d.cfg.Firewall
	d.mu.Unlock()
	if !f.Enabled {
		return
	}
	d.fw.mu.Lock()
	defer d.fw.mu.Unlock()
	if d.fw.pending != nil {
		return
	}
	script := d.fwScript(f, forceResolve)
	if script == d.fw.applied {
		return
	}
	if err := d.fwApplyLocked(script); err != nil {
		slog.Error("firewall: cannot update the rules", "error", err)
	} else {
		slog.Info("firewall: rules updated (connection hosts changed)")
	}
}

// fwRevertLocked restores the confirmed settings; requires d.fw.mu.
func (d *Daemon) fwRevertLocked(reason string) {
	p := d.fw.pending
	if p == nil {
		return
	}
	p.timer.Stop()
	d.fw.pending = nil
	if err := d.fwApplyLocked(d.fwScript(p.prev, false)); err != nil {
		slog.Error("firewall: cannot restore the previous rules", "error", err)
		return
	}
	slog.Warn("firewall: change reverted", "reason", reason)
}

// --- HTTP ---

func (d *Daemon) handleFirewallGet(w http.ResponseWriter, r *http.Request) {
	reply(w, d.firewallStatus())
}

func (d *Daemon) firewallStatus() api.FirewallStatus {
	smb := d.smbHosts(false)
	d.mu.Lock()
	settings := d.cfg.Firewall
	d.mu.Unlock()

	d.fw.mu.Lock()
	st := api.FirewallStatus{Available: d.fwAvailable(), Error: d.fw.lastErr, Settings: settings}
	if p := d.fw.pending; p != nil {
		st.Settings, st.PendingUntil = p.next, p.deadline
	}
	d.fw.mu.Unlock()

	if st.Settings.Enabled {
		st.Rules = firewall.Plan(st.Settings, smb)
	}
	if st.Available {
		counters, exists, err := firewall.Counters(d.fw.runner)
		if err != nil && st.Error == "" {
			st.Error = err.Error()
		}
		st.Active = exists
		for i := range st.Rules {
			c := counters[st.Rules[i].ID]
			st.Rules[i].Packets, st.Rules[i].Bytes = c[0], c[1]
		}
	}
	if l, c, err := firewall.Sockets(); err == nil {
		st.Listening, st.Connections = l, c
	} else if st.Error == "" {
		st.Error = "ss: " + err.Error()
	}
	d.fw.hostsMu.Lock()
	st.HostNames = d.fw.hostNames
	if st.Settings.Enabled {
		st.Unresolved = d.fw.unresolved
	}
	d.fw.hostsMu.Unlock()
	return st
}

func (d *Daemon) handleFirewallSet(w http.ResponseWriter, r *http.Request) {
	var next config.Firewall
	if err := decode(r, &next); err != nil {
		fail(w, 400, err)
		return
	}
	if err := next.Validate(); err != nil {
		fail(w, 400, err)
		return
	}
	d.mu.Lock()
	confirmed := d.cfg.Firewall
	d.mu.Unlock()

	d.fw.mu.Lock()
	if p := d.fw.pending; p != nil {
		// a new change replaces the pending one: the reference stays the confirmed settings
		p.timer.Stop()
		d.fw.pending = nil
	}
	if !next.Enabled {
		// turning the firewall off cannot lock anyone out: apply and save right away
		err := d.fwApplyLocked(firewall.DisableScript())
		d.fw.mu.Unlock()
		if err != nil {
			fail(w, 500, err)
			return
		}
		if err := d.saveFirewall(next); err != nil {
			fail(w, 500, err)
			return
		}
		slog.Info("firewall: disabled")
		reply(w, d.firewallStatus())
		return
	}
	if err := d.fwApplyLocked(d.fwScript(next, true)); err != nil {
		// the script failed as a whole: the previous rules are still in place
		d.fw.mu.Unlock()
		fail(w, 400, err)
		return
	}
	p := &fwPending{next: next, prev: confirmed, deadline: time.Now().Add(ConfirmTimeout)}
	p.timer = time.AfterFunc(ConfirmTimeout, func() {
		d.fw.mu.Lock()
		defer d.fw.mu.Unlock()
		if d.fw.pending == p {
			d.fwRevertLocked("not confirmed in time")
		}
	})
	d.fw.pending = p
	d.fw.mu.Unlock()
	slog.Info("firewall: new rules applied, waiting for confirmation", "deadline", p.deadline.Format(time.TimeOnly))
	reply(w, d.firewallStatus())
}

func (d *Daemon) handleFirewallConfirm(w http.ResponseWriter, r *http.Request) {
	d.fw.mu.Lock()
	p := d.fw.pending
	if p == nil {
		d.fw.mu.Unlock()
		fail(w, 409, errors.New(T("there is no firewall change to confirm")))
		return
	}
	p.timer.Stop()
	d.fw.pending = nil
	d.fw.mu.Unlock()
	if err := d.saveFirewall(p.next); err != nil {
		fail(w, 500, err)
		return
	}
	slog.Info("firewall: change confirmed")
	reply(w, map[string]bool{"ok": true})
}

func (d *Daemon) handleFirewallRevert(w http.ResponseWriter, r *http.Request) {
	d.fw.mu.Lock()
	defer d.fw.mu.Unlock()
	if d.fw.pending == nil {
		fail(w, 409, errors.New(T("there is no firewall change to revert")))
		return
	}
	d.fwRevertLocked("reverted by the user")
	reply(w, map[string]bool{"ok": true})
}

// saveFirewall stores confirmed settings in the configuration.
func (d *Daemon) saveFirewall(f config.Firewall) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := cloneConfig(d.cfg)
	d.cfg.Firewall = f
	return d.saveConfigLocked(prev)
}

// unresolvedWarning reports connection hosts that cannot be resolved while the
// firewall is on: outgoing SMB to them is blocked, so their syncs would fail.
func (d *Daemon) unresolvedWarning() string {
	d.mu.Lock()
	on := d.cfg.Firewall.Enabled
	d.mu.Unlock()
	if !on {
		return ""
	}
	d.fw.hostsMu.Lock()
	defer d.fw.hostsMu.Unlock()
	if len(d.fw.unresolved) == 0 {
		return ""
	}
	return Tf("firewall: cannot resolve %s: SMB to these hosts is blocked (use IP addresses in the connections)",
		strings.Join(d.fw.unresolved, ", "))
}
