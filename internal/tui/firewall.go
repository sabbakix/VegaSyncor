package tui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/firewall"
)

// ---------- messages and commands ----------

type fwMsg struct {
	st  *api.FirewallStatus
	err error
}

type fwSavedMsg struct {
	st  *api.FirewallStatus
	ok  string
	err error
}

type fwActionMsg struct {
	ok  string
	err error
}

func (m *Model) fetchFirewall() tea.Cmd {
	m.fwPolling = true
	c := m.client
	return func() tea.Msg {
		st, err := c.Firewall()
		return fwMsg{st, err}
	}
}

// saveFirewall applies new settings; when the firewall is enabled the service
// waits for a confirmation (see viewFwConfirm).
func (m *Model) saveFirewall(next config.Firewall, ok string) tea.Cmd {
	m.saving = true
	c := m.client
	return func() tea.Msg {
		st, err := c.SetFirewall(next)
		return fwSavedMsg{st, ok, err}
	}
}

// updateFirewall handles the firewall messages; ok=false if msg is not one of them.
func (m *Model) updateFirewall(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case fwMsg:
		m.fwPolling = false
		m.fwErr = msg.err
		if msg.err == nil {
			m.fw = msg.st
			m.fwCur = clamp(m.fwCur, len(m.fwItems()))
		}
		return nil, true
	case fwSavedMsg:
		m.saving = false
		if msg.err != nil {
			if m.form != nil {
				m.form.Err = msg.err.Error()
			} else {
				m.setFlash(msg.err.Error(), true)
			}
			return nil, true
		}
		m.form = nil
		m.fw = msg.st
		if !msg.st.Settings.Enabled && msg.ok != "" {
			m.setFlash(msg.ok, false)
		}
		return nil, true
	case fwActionMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error(), true)
		} else if msg.ok != "" {
			m.setFlash(msg.ok, false)
		}
		return m.fetchFirewall(), true
	}
	return nil, false
}

// fwPendingLeft returns how long until an unconfirmed change is reverted.
func (m *Model) fwPendingLeft() (time.Duration, bool) {
	if m.fw == nil || m.fw.PendingUntil.IsZero() {
		return 0, false
	}
	return m.fw.PendingUntil.Sub(m.serverNow()), true
}

// sshClientIP is the address of the current SSH session, if any (to propose it
// as admin host and to warn before locking it out).
func sshClientIP() string {
	if v := os.Getenv("SSH_CLIENT"); v != "" {
		return strings.Fields(v)[0]
	}
	return ""
}

// ---------- items of the tab ----------

type fwItem struct {
	kind string // "listen" | "conn" | "rule"
	sock firewall.Socket
	rule firewall.Rule
}

func (m *Model) fwItems() []fwItem {
	if m.fw == nil {
		return nil
	}
	var out []fwItem
	for _, s := range m.fw.Listening {
		out = append(out, fwItem{kind: "listen", sock: s})
	}
	for _, s := range m.fw.Connections {
		out = append(out, fwItem{kind: "conn", sock: s})
	}
	for _, r := range m.fw.Rules {
		out = append(out, fwItem{kind: "rule", rule: r})
	}
	return out
}

// fwAccess tells whether a listening port can be reached from the network.
func (m *Model) fwAccess(s firewall.Socket) (string, bool) {
	if s.LocalOnly {
		return T("local only"), true
	}
	st := m.fw
	if !st.Settings.Enabled || !st.Active {
		return T("open to the network"), false
	}
	f := st.Settings
	hosts := func(h []string) string {
		if len(h) == 0 {
			return T("any host")
		}
		return strings.Join(h, ", ")
	}
	if s.Proto == "tcp" && s.LocalPort == f.SSH() {
		return T("allowed:") + " " + hosts(f.AdminHosts), true
	}
	if s.Proto == "udp" && s.LocalPort == 68 {
		return T("allowed: DHCP"), true
	}
	for _, r := range f.Rules {
		if r.Direction == config.FwIn && r.Action == config.FwAllow && protoMatch(r.Proto, s.Proto) && portMatch(r.Port, s.LocalPort) {
			h := []string{}
			if r.Host != "" {
				h = []string{r.Host}
			}
			return T("allowed:") + " " + hosts(h), true
		}
	}
	return T("blocked"), true
}

func protoMatch(rule, proto string) bool { return rule == config.FwAny || rule == proto }

func portMatch(rule string, port int) bool {
	if rule == "" {
		return true
	}
	lo, hi, isRange := strings.Cut(rule, "-")
	a, _ := strconv.Atoi(lo)
	b := a
	if isRange {
		b, _ = strconv.Atoi(hi)
	}
	return port >= a && port <= b
}

func (m *Model) hostLabel(ip string) string {
	if m.fw != nil && m.fw.HostNames[ip] != "" {
		return m.fw.HostNames[ip] + " " + ip
	}
	return ip
}

// ruleText describes a rule for the list.
func ruleText(r firewall.Rule) string {
	ports := strings.Join(r.Ports, ",")
	switch r.Kind {
	case firewall.KindLoopback:
		return T("local traffic (loopback)")
	case firewall.KindEstablished:
		return T("replies to allowed connections")
	case firewall.KindInvalid:
		return T("invalid packets")
	case firewall.KindSSH:
		return Tf("SSH (port %s)", ports)
	case firewall.KindPing:
		return T("ping")
	case firewall.KindICMPv6:
		return T("IPv6 control messages")
	case firewall.KindDHCP:
		return T("DHCP (network address)")
	case firewall.KindDNS:
		return T("DNS (name resolution)")
	case firewall.KindNTP:
		return T("time sync (NTP)")
	case firewall.KindWeb:
		return T("web: updates (HTTP/HTTPS)")
	case firewall.KindSMB:
		return T("SMB to the connection hosts")
	case firewall.KindDrop:
		return T("everything else (logged)")
	}
	proto := r.Proto
	if proto == config.FwAny {
		proto = "tcp/udp"
	}
	s := proto
	if ports != "" {
		s += " " + ports
	}
	if r.Comment != "" {
		s += " – " + r.Comment
	}
	return s
}

func dirText(d string) string {
	if d == config.FwIn {
		return T("in")
	}
	return T("out")
}

// ---------- view ----------

func (m *Model) viewFirewall() (string, string) {
	help := T("e settings · a add rule · enter edit/add · d delete rule · r refresh · tab next tab · L language · q quit")
	if m.fw == nil {
		if m.fwErr != nil {
			return "\n  " + sErr.Render(m.fwErr.Error()), help
		}
		return "\n  " + sMuted.Render(T("loading…")), help
	}
	st := m.fw
	w := m.w
	var b strings.Builder

	// status box
	var status []string
	switch {
	case !st.Available:
		status = append(status, sErr.Render(T("nftables is not installed: the firewall cannot be enabled (apt install nftables)")))
	case st.Settings.Enabled && st.Active:
		var in, out uint64
		for _, r := range st.Rules {
			if r.Kind == firewall.KindDrop {
				if r.Direction == config.FwIn {
					in = r.Packets
				} else {
					out = r.Packets
				}
			}
		}
		ssh := T("any host")
		if len(st.Settings.AdminHosts) > 0 {
			ssh = strings.Join(st.Settings.AdminHosts, ", ")
		}
		status = append(status, sOK.Render(T("Firewall ACTIVE (lockdown)"))+sMuted.Render("  ·  ")+
			Tf("SSH from: %s", ssh)+sMuted.Render("  ·  ")+Tf("blocked: %d in, %d out", in, out))
	case st.Settings.Enabled:
		status = append(status, sWarn.Render(T("Firewall enabled but the rules are not loaded")))
	default:
		status = append(status, sWarn.Render(T("Firewall OFF: every listening port is reachable from the network."))+
			"  "+Tf("Press %s to enable the lockdown.", sKey.Render("e")))
	}
	if len(st.Unresolved) > 0 {
		status = append(status, sWarn.Render(trunc(Tf("Cannot resolve %s: SMB to these hosts is blocked. Use IP addresses in the connections.",
			strings.Join(st.Unresolved, ", ")), w-6)))
	}
	if st.Error != "" {
		status = append(status, sErr.Render(trunc(T("Error:")+" "+st.Error, w-6)))
	}
	b.WriteString(sBox.Width(w-2).Render(strings.Join(status, "\n")) + "\n")

	items := m.fwItems()
	var lines []string
	section := func(title string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sSection.Render(title)+" "+rule(w-len([]rune(title))-3))
	}
	idx, focus := 0, 0
	add := func(i int, row string) {
		line := "   " + row
		if i == m.fwCur {
			line = " " + sKey.Render(">") + " " + sSel.Render(row)
			focus = len(lines)
		}
		lines = append(lines, zone.Mark(fmt.Sprintf("fw:%d", i), line))
	}

	section(T("LISTENING PORTS"))
	lines = append(lines, sMuted.Render("   "+pad(T("PROTO"), 6)+" "+pad(T("PORT"), 6)+" "+pad(T("ADDRESS"), 22)+" "+
		pad(T("PROCESS"), 18)+" "+T("ACCESS")))
	for ; idx < len(items) && items[idx].kind == "listen"; idx++ {
		s := items[idx].sock
		access, safe := m.fwAccess(s)
		row := pad(s.Proto, 6) + " " + pad(strconv.Itoa(s.LocalPort), 6) + " " + pad(s.LocalAddr, 22) + " " +
			pad(orDash(s.Process), 18) + " "
		if idx == m.fwCur {
			row += pad(access, max(w-60, 10))
		} else if safe {
			row += sMuted.Render(access)
		} else {
			row += sWarn.Render(access)
		}
		add(idx, row)
	}
	if len(st.Listening) == 0 {
		lines = append(lines, sMuted.Render("   "+T("(none)")))
	}

	section(T("ACTIVE CONNECTIONS"))
	lines = append(lines, sMuted.Render("   "+pad(T("DIR"), 5)+" "+pad(T("PROTO"), 6)+" "+pad(T("REMOTE HOST"), 40)+" "+
		pad(T("PORT"), 6)+" "+T("PROCESS")))
	for ; idx < len(items) && items[idx].kind == "conn"; idx++ {
		s := items[idx].sock
		port := s.RemotePort
		if s.Direction == "in" {
			port = s.LocalPort
		}
		proc := s.Process
		if proc == "" {
			proc = T("kernel")
			if port == 445 || port == 139 {
				proc = T("kernel (SMB mount)")
			}
		}
		row := pad(dirText(s.Direction), 5) + " " + pad(s.Proto, 6) + " " + pad(m.hostLabel(s.RemoteAddr), 40) + " " +
			pad(strconv.Itoa(port), 6) + " " + pad(proc, max(w-66, 8))
		add(idx, row)
	}
	if len(st.Connections) == 0 {
		lines = append(lines, sMuted.Render("   "+T("(none)")))
	}

	if len(st.Rules) > 0 {
		section(T("RULES"))
		lines = append(lines, sMuted.Render("   "+pad(T("DIR"), 5)+" "+pad(T("ACTION"), 7)+" "+pad(T("WHAT"), 36)+" "+
			pad(T("HOSTS"), 30)+" "+T("PACKETS")))
		for ; idx < len(items) && items[idx].kind == "rule"; idx++ {
			r := items[idx].rule
			action := T("allow")
			if r.Action == config.FwBlock {
				action = T("block")
			}
			hosts := T("any")
			if len(r.Hosts) > 0 {
				var hs []string
				for _, h := range r.Hosts {
					hs = append(hs, m.hostLabelShort(h))
				}
				hosts = strings.Join(hs, ", ")
			}
			what := ruleText(r)
			if !r.Builtin {
				what = "* " + what
			}
			row := pad(dirText(r.Direction), 5) + " " + pad(action, 7) + " " + pad(what, 36) + " " +
				pad(hosts, 30) + " " + pad(strconv.FormatUint(r.Packets, 10), max(w-86, 6))
			if idx != m.fwCur {
				switch {
				case r.Action == config.FwBlock:
					row = sErr.Render(row)
				case r.Builtin:
					row = sMuted.Render(row)
				}
			}
			add(idx, row)
		}
		lines = append(lines, sMuted.Render("   "+T("* = your rules (enter edit, d delete); the others come from the settings (e)")))
	}

	listH := max(m.h-6-m.warningsHeight()-helpExtra(help, m.w)-lipgloss.Height(b.String()), 3)
	m.fwOffset = listWindow(focus, m.fwOffset, len(lines), listH)
	end := min(len(lines), m.fwOffset+listH)
	b.WriteString(strings.Join(lines[m.fwOffset:end], "\n"))
	return b.String(), help
}

func (m *Model) hostLabelShort(h string) string {
	if m.fw != nil && m.fw.HostNames[h] != "" {
		name := m.fw.HostNames[h]
		if i := strings.Index(name, " ("); i > 0 {
			name = name[:i]
		}
		return name
	}
	return h
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// viewFwConfirm is the dialog asking to keep the rules just applied.
func (m *Model) viewFwConfirm(w int) string {
	left, _ := m.fwPendingLeft()
	secs := int(left.Seconds())
	countdown := Tf("They will be reverted automatically in %d s.", max(secs, 0))
	if secs <= 0 {
		countdown = T("Reverting…")
	}
	body := sBold.Render(T("New firewall rules applied.")) + "\n\n" +
		T("If you can still use this session, the rules do not lock you out: keep them.") + "\n" +
		T("If the connection was lost, do nothing:") + " " + sWarn.Render(countdown)
	return sFocusBox.Width(min(w, 80)).Render(body + "\n\n" + renderHelp(T("y keep the rules · n revert now"), w))
}

// fwConfirmKey handles the keys of the confirmation dialog; ok=false if no change is pending.
func (m *Model) fwConfirmKey(key string) (tea.Cmd, bool) {
	if _, pending := m.fwPendingLeft(); !pending || m.form != nil {
		return nil, false
	}
	c := m.client
	switch key {
	case "y", "Y", "s", "S":
		m.fw.PendingUntil = time.Time{}
		return func() tea.Msg { return fwActionMsg{T("Firewall rules confirmed."), c.ConfirmFirewall()} }, true
	case "n", "N", "esc":
		m.fw.PendingUntil = time.Time{}
		return func() tea.Msg { return fwActionMsg{T("Firewall change reverted."), c.RevertFirewall()} }, true
	case "ctrl+c", "q":
		return nil, false
	}
	return nil, true // other keys are ignored while the dialog is open
}

// ---------- keys ----------

func (m *Model) firewallKey(key string) tea.Cmd {
	items := m.fwItems()
	n := len(items)
	switch key {
	case "up", "k":
		m.fwCur = clamp(m.fwCur-1, n)
	case "down", "j":
		m.fwCur = clamp(m.fwCur+1, n)
	case "pgup":
		m.fwCur = clamp(m.fwCur-10, n)
	case "pgdown":
		m.fwCur = clamp(m.fwCur+10, n)
	case "r":
		return m.fetchFirewall()
	case "e":
		if m.fw != nil {
			m.form, m.formKind, m.formID = newFwSettingsForm(m.fw.Settings), "fwsettings", ""
		}
	case "a":
		if m.fw != nil {
			var it *fwItem
			if m.fwCur < n {
				it = &items[m.fwCur]
			}
			m.form, m.formKind, m.formID = newFwRuleForm(ruleFromItem(it)), "fwrule", ""
		}
	case "enter":
		if m.fwCur >= n {
			return nil
		}
		it := items[m.fwCur]
		if it.kind == "rule" {
			if it.rule.Builtin {
				m.setFlash(T("Built-in rule: change it from the settings (e)"), true)
				return nil
			}
			if r := m.userRule(it.rule.ID); r != nil {
				m.form, m.formKind, m.formID = newFwRuleForm(r), "fwrule", r.ID
			}
			return nil
		}
		m.form, m.formKind, m.formID = newFwRuleForm(ruleFromItem(&it)), "fwrule", ""
	case "d", "delete":
		if m.fwCur < n && items[m.fwCur].kind == "rule" && !items[m.fwCur].rule.Builtin {
			if r := m.userRule(items[m.fwCur].rule.ID); r != nil {
				id := r.ID
				m.confirm = &confirmBox{
					text: T("Delete this firewall rule?"),
					action: func() tea.Msg {
						next := m.fw.Settings
						next.Rules = nil
						for _, x := range m.fw.Settings.Rules {
							if x.ID != id {
								next.Rules = append(next.Rules, x)
							}
						}
						st, err := m.client.SetFirewall(next)
						return fwSavedMsg{st, T("Rule deleted."), err}
					},
				}
			}
		}
	}
	return nil
}

// userRule finds the configured rule behind a rule of the list ("rule-<id>").
func (m *Model) userRule(planID string) *config.FwRule {
	id := strings.TrimPrefix(planID, "rule-")
	for i := range m.fw.Settings.Rules {
		if m.fw.Settings.Rules[i].ID == id {
			r := m.fw.Settings.Rules[i]
			return &r
		}
	}
	return nil
}

// ruleFromItem pre-fills a new rule from the selected port or connection.
func ruleFromItem(it *fwItem) *config.FwRule {
	r := &config.FwRule{Direction: config.FwIn, Action: config.FwAllow, Proto: config.FwTCP}
	if it == nil {
		return r
	}
	switch it.kind {
	case "listen":
		r.Proto, r.Port = it.sock.Proto, strconv.Itoa(it.sock.LocalPort)
	case "conn":
		// vetting a connection: by default the rule blocks its remote host
		r.Action, r.Proto, r.Direction, r.Host = config.FwBlock, it.sock.Proto, it.sock.Direction, it.sock.RemoteAddr
		if it.sock.Direction == "in" {
			r.Port = strconv.Itoa(it.sock.LocalPort)
		} else {
			r.Port = strconv.Itoa(it.sock.RemotePort)
		}
	}
	return r
}

// ---------- forms ----------

func newFwSettingsForm(f config.Firewall) *form {
	if !f.Enabled && f.SSHPort == 0 && len(f.Rules) == 0 && len(f.AdminHosts) == 0 {
		def := config.DefaultFirewall()
		def.Rules = f.Rules
		f = def
	}
	hosts := f.AdminHosts
	if !f.Enabled && len(hosts) == 0 && sshClientIP() != "" {
		hosts = []string{sshClientIP()} // propose the address of this session
	}
	hostHelp := T("IP addresses or networks allowed to use SSH, comma-separated; empty = any host")
	if ip := sshClientIP(); ip != "" {
		hostHelp += " · " + Tf("this session comes from %s", ip)
	}
	fm := &form{Title: T("Firewall settings"), Fields: []*field{
		section(T("Lockdown")),
		newBool("enabled", T("Firewall active"), f.Enabled).
			withHelp(T("incoming: only SSH from the admin hosts; outgoing: only SMB to the connection hosts and basic services; everything else blocked")),
		newText("ssh_port", T("SSH port"), strconv.Itoa(f.SSH()), "22"),
		newText("admin_hosts", T("Admin hosts (SSH)"), strings.Join(hosts, ", "), T("e.g. 192.168.1.20, 192.168.1.0/24")).withHelp(hostHelp),
		newBool("allow_web", T("Allow web (updates)"), f.AllowWeb).
			withHelp(T("outgoing HTTP/HTTPS, needed for system updates and to update VegaSyncor")),
		newBool("allow_ping", T("Answer ping"), f.AllowPing).withHelp(T("lets other hosts check that the server is up")),
	}}
	fm.init()
	return fm
}

func fwSettingsFromForm(fm *form, base config.Firewall) (config.Firewall, error) {
	f := base
	f.Enabled = fm.get("enabled").Bool
	f.AllowWeb = fm.get("allow_web").Bool
	f.AllowPing = fm.get("allow_ping").Bool
	port, err := strconv.Atoi(strings.TrimSpace(fm.val("ssh_port")))
	if err != nil || port < 1 || port > 65535 {
		return f, errors.New(T("invalid SSH port"))
	}
	f.SSHPort = port
	f.AdminHosts = nil
	for _, h := range strings.FieldsFunc(fm.val("admin_hosts"), func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		f.AdminHosts = append(f.AdminHosts, h)
	}
	return f, nil
}

func newFwRuleForm(r *config.FwRule) *form {
	title := T("New firewall rule")
	if r.ID != "" {
		title = T("Edit firewall rule")
	}
	dirs := []option{{config.FwIn, T("Incoming (to this server)")}, {config.FwOut, T("Outgoing (from this server)")}}
	actions := []option{{config.FwAllow, T("Allow")}, {config.FwBlock, T("Block")}}
	protos := []option{{config.FwTCP, "TCP"}, {config.FwUDP, "UDP"}, {config.FwAny, T("TCP and UDP")}}
	fm := &form{Title: title, Fields: []*field{
		section(T("Rule")),
		newChoice("direction", T("Direction"), dirs, r.Direction),
		newChoice("action", T("Action"), actions, r.Action).
			withHelp(T("block rules are checked before the allow rules, so they win")),
		newChoice("proto", T("Protocol"), protos, r.Proto),
		newText("port", T("Port"), r.Port, T("e.g. 445 or 8000-8100; empty = all ports")).
			withHelp(T("incoming: port of this server; outgoing: port of the remote host")),
		newText("host", T("Remote host"), r.Host, T("e.g. 192.168.1.50 or 192.168.1.0/24; empty = any host")),
		newText("comment", T("Note"), r.Comment, T("optional, e.g. monitoring server")),
	}}
	fm.init()
	return fm
}

func fwRuleFromForm(fm *form, id string) config.FwRule {
	return config.FwRule{
		ID: id, Direction: fm.choice("direction"), Action: fm.choice("action"), Proto: fm.choice("proto"),
		Port: fm.val("port"), Host: fm.val("host"), Comment: fm.val("comment"),
	}
}

// saveFirewallForm builds the new settings from the open form and applies them.
func (m *Model) saveFirewallForm() tea.Cmd {
	fm := m.form
	base := m.fw.Settings
	switch m.formKind {
	case "fwsettings":
		next, err := fwSettingsFromForm(fm, base)
		if err != nil {
			fm.Err = err.Error()
			return nil
		}
		if next.Enabled {
			if err := next.Validate(); err != nil {
				fm.Err = err.Error()
				return nil
			}
			if ip := sshClientIP(); ip != "" && !hostAllowed(next.AdminHosts, ip) {
				fm.Err = Tf("warning: %s (this session) is not in the admin hosts; if you save, the change is reverted unless you confirm it", ip)
				if !fm.warned {
					fm.warned = true
					return nil // second ctrl+s saves anyway
				}
			}
		}
		return m.saveFirewall(next, T("Firewall settings saved."))
	case "fwrule":
		r := fwRuleFromForm(fm, m.formID)
		if err := r.Validate(); err != nil {
			fm.Err = err.Error()
			return nil
		}
		next := base
		next.Rules = nil
		replaced := false
		for _, x := range base.Rules {
			if x.ID == r.ID {
				x, replaced = r, true
			}
			next.Rules = append(next.Rules, x)
		}
		if !replaced {
			next.Rules = append(next.Rules, r)
		}
		ok := T("Rule saved.")
		if !next.Enabled {
			ok = T("Rule saved: it becomes active when the firewall is enabled.")
		}
		return m.saveFirewall(next, ok)
	}
	return nil
}

// hostAllowed reports whether ip is covered by the admin hosts (empty = any).
func hostAllowed(hosts []string, ip string) bool {
	if len(hosts) == 0 {
		return true
	}
	for _, h := range hosts {
		if h == ip {
			return true
		}
		if _, n, err := net.ParseCIDR(h); err == nil && n.Contains(net.ParseIP(ip)) {
			return true
		}
	}
	return false
}
