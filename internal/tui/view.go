package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
)

// View draws the screen; zone.Scan records the positions of the clickable parts.
func (m *Model) View() string {
	helpKeys = helpKeys[:0]
	return paintBackground(zone.Scan(m.render()), m.w, m.h)
}

func (m *Model) render() string {
	if m.w == 0 {
		return T("loading…")
	}
	var body, help string
	switch {
	case m.logOpen:
		return m.viewLog()
	case m.form != nil:
		body, help = m.viewForm()
	case m.st == nil && m.connErr != nil:
		body, help = m.viewNoDaemon(), T("q quit")
	case m.st == nil:
		body = sMuted.Render("  " + T("connecting to the service…"))
	default:
		switch m.tab {
		case tabJobs:
			body, help = m.viewJobs()
		case tabConns:
			body, help = m.viewConns()
		case tabHistory:
			body, help = m.viewHistory()
		case tabFirewall:
			body, help = m.viewFirewall()
		}
	}

	header := m.viewHeader()
	footer := m.viewFooter(help)
	bodyH := m.h - lipgloss.Height(header) - lipgloss.Height(footer)
	if m.form != nil && !m.logOpen {
		// description bar anchored at the bottom, above the keys
		body = fitHeight(body, bodyH-formDescHeight) + "\n" + m.viewFormDescription()
	} else {
		body = fitHeight(body, bodyH)
	}

	screen := header + "\n" + body + "\n" + footer
	if ov := m.overlay(); ov != "" {
		return placeOverlay(m.w, m.h, ov)
	}
	return screen
}

func fitHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:max(h, 0)]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// placeOverlay centres a modal window on the screen.
func placeOverlay(w, h int, box string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func (m *Model) viewHeader() string {
	left := sTitle.Render(" VegaSyncor ")
	var tabs []string
	for i, n := range tabNames() {
		st := sTabOff
		if tab(i) == m.tab && m.form == nil {
			st = sTabOn
		}
		tabs = append(tabs, zone.Mark(fmt.Sprintf("tab:%d", i), st.Render(n)))
	}
	// right-hand parts by importance: clock and language always stay, then state and host name
	var state, clock, clockShort, host string
	lang := m.viewLanguages()
	switch {
	case m.connErr != nil:
		state = sErr.Render(T("service unreachable"))
	case m.st != nil:
		running := 0
		for _, j := range m.st.Jobs {
			if j.Current != nil {
				running++
			}
		}
		state = sOK.Render(T("service running"))
		if running > 0 {
			state = sRun.Render(Tf("%d running", running))
		}
		now := m.serverNow()
		clock = sClock.Render(fmtClock(now, m.st.ZoneAbbr))
		clockShort = sClock.Render(now.Format(i18n.ShortDateTimeLayout() + ":05"))
		host = sMuted.Render(m.st.Hostname)
	}
	line := left + " " + strings.Join(tabs, " ")
	var right string
	for _, parts := range [][]string{{state, clock, lang, host}, {state, clock, lang}, {clock, lang}, {clockShort, lang}, {clockShort}} {
		var nonEmpty []string
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		right = strings.Join(nonEmpty, "   ") + " "
		if lipgloss.Width(line)+lipgloss.Width(right)+1 <= m.w {
			break
		}
	}
	gap := m.w - lipgloss.Width(line) - lipgloss.Width(right)
	if gap < 1 {
		right = ""
		gap = 1
	}
	out := line + strings.Repeat(" ", gap) + right + "\n" + rule(m.w)
	if m.st != nil && len(m.st.Warnings) > 0 && m.form == nil {
		out += "\n" + m.viewWarnings()
	}
	return out
}

func (m *Model) warningsHeight() int {
	if m.st == nil || len(m.st.Warnings) == 0 {
		return 0
	}
	return lipgloss.Height(m.viewWarnings())
}

// viewWarnings highlights the environment problems reported by the service
// (e.g. unprivileged container), with the full text wrapped.
func (m *Model) viewWarnings() string {
	inner := max(m.w-6, 20)
	var parts []string
	for _, w := range m.st.Warnings {
		txt := lipgloss.NewStyle().Width(inner - 2).Render(w)
		lines := strings.Split(txt, "\n")
		for i, l := range lines {
			prefix := "  "
			if i == 0 {
				prefix = "! "
			}
			lines[i] = sWarn.Render(prefix + strings.TrimRight(l, " "))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cWarn).
		Padding(0, 1).Width(m.w - 2)
	return box.Render(strings.Join(parts, "\n"))
}

func (m *Model) viewFooter(help string) string {
	msg := ""
	if m.flash != "" {
		if m.flashErr {
			msg = sErr.Render(" " + T("ERROR:") + " " + trunc(m.flash, m.w-10))
		} else {
			msg = sOK.Render(" OK: " + trunc(m.flash, m.w-6))
		}
	} else if m.connErr != nil && m.st != nil {
		msg = sErr.Render(" " + T("ERROR:") + " " + trunc(m.connErr.Error(), m.w-10))
	}
	return msg + "\n" + rule(m.w) + "\n" + renderHelp(help, m.w)
}

// helpExtra returns the extra lines taken by the command bar when it wraps.
func helpExtra(help string, w int) int {
	n := len(helpKeys)
	h := lipgloss.Height(renderHelp(help, w))
	helpKeys = helpKeys[:n] // measuring must not register clickable keys
	return max(h-1, 0)
}

// renderHelp highlights the keys: format "key description · key description".
func renderHelp(h string, w int) string {
	if h == "" {
		return ""
	}
	var parts []string
	for _, p := range strings.Split(h, " · ") {
		k, d, _ := strings.Cut(p, " ")
		item := sKey.Render(k) + " " + sMuted.Render(d)
		if _, ok := keyForLabel(k); ok {
			item = zone.Mark("key:"+k, item)
			helpKeys = append(helpKeys, k)
		}
		parts = append(parts, item)
	}
	// separator always visible; if the items do not fit on one line, wrap
	sep := sSep.Render(" │ ")
	var lines []string
	line := ""
	for _, p := range parts {
		switch {
		case line == "":
			line = " " + p
		case lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(p) <= w:
			line += sep + p
		default:
			lines = append(lines, line)
			line = " " + p
		}
	}
	return strings.Join(append(lines, line), "\n")
}

func (m *Model) viewNoDaemon() string {
	msg := sErr.Render(T("Cannot contact the VegaSyncor service")) + "\n\n" +
		m.connErr.Error() + "\n\n" +
		sMuted.Render(T("Check that it is running:")) + "\n" +
		"  sudo systemctl status vegasyncor\n  sudo systemctl start vegasyncor\n\n" +
		sMuted.Render(T("The TUI retries automatically every second."))
	return "\n" + lipgloss.NewStyle().MarginLeft(2).Render(sBox.Render(msg))
}

// ---------- scheda sincronizzazioni ----------

func scheduleText(j api.JobStatus) string {
	if !j.Job.Enabled {
		return T("paused")
	}
	return j.Job.Schedule.Describe()
}

func (m *Model) viewJobs() (string, string) {
	help := T("n new · enter edit · r run · s dry-run · x stop · p pause · l log · d delete · tab next tab · L language · q quit")
	jobs := m.jobs()
	if len(jobs) == 0 {
		var b strings.Builder
		b.WriteString("\n  " + sBold.Render(T("No syncs configured.")) + "\n\n")
		if len(m.conns()) == 0 {
			b.WriteString("  " + T("To get started:") + "\n")
			b.WriteString("    " + Tf("1. go to the %s tab and press %s to save the credentials of a PC or server",
				sKey.Render(tabNames()[1]), sKey.Render("n")) + "\n")
			b.WriteString("    " + Tf("2. come back here and press %s to create the first sync", sKey.Render("n")) + "\n")
		} else {
			b.WriteString("  " + Tf("Press %s to create the first sync.", sKey.Render("n")) + "\n")
		}
		return b.String(), T("n new · tab next tab · L language · q quit")
	}

	w := m.w
	// columns: tag, name, when, last, next + route (the rest)
	nameW, whenW, lastW, nextW := 22, 20, 18, 13
	if w < 100 {
		nameW, lastW, nextW = 16, 13, 12
	}
	showWhen := w >= 110
	fixed := 7 + nameW + lastW + nextW + 4
	if showWhen {
		fixed += whenW + 1
	}
	routeW := max(w-fixed, 16)

	var b strings.Builder
	hdr := "       " + pad(T("NAME"), nameW) + " " + pad(T("SOURCE  →  DESTINATION"), routeW) + " "
	if showWhen {
		hdr += pad(T("WHEN"), whenW) + " "
	}
	hdr += pad(T("LAST"), lastW) + " " + pad(T("NEXT"), nextW)
	b.WriteString(sMuted.Render(hdr) + "\n")

	listH := max(m.h-22-m.warningsHeight()-helpExtra(help, m.w), 3)
	off := listWindow(m.jobCur, 0, len(jobs), listH)
	for i := off; i < len(jobs) && i < off+listH; i++ {
		j := jobs[i]
		icon := statusTag("")
		switch {
		case j.Current != nil:
			icon = statusTag(api.StatusRunning)
		case !j.Job.Enabled:
			icon = sMuted.Render("[OFF]")
		case j.Last != nil:
			icon = statusTag(j.Last.Status)
		}
		half := (routeW - 3) / 2
		route := pad(truncLeft(j.Source, half), half) + " → " + truncLeft(j.Dest, routeW-half-3)
		last := T("never run")
		if j.Current != nil {
			last = T("running")
			if p := j.Current.Progress; p != nil {
				last = Tf("running %d%%", p.Percent)
			}
		} else if j.Last != nil {
			last = fmtTime(j.Last.Start)
			if j.Last.DryRun {
				last += " (sim)"
			}
		}
		next := "–"
		if !j.Next.IsZero() {
			next = fmtTime(j.Next)
		}
		row := pad(j.Job.Name, nameW) + " " + pad(route, routeW) + " "
		if showWhen {
			row += pad(scheduleText(j), whenW) + " "
		}
		row += pad(last, lastW) + " " + pad(next, nextW)
		if i == m.jobCur {
			row = sSel.Render(row)
		}
		b.WriteString(zone.Mark(fmt.Sprintf("job:%d", i), " "+icon+" "+row) + "\n")
	}
	if sel := m.selJob(); sel != nil {
		b.WriteString("\n" + m.viewJobDetail(*sel))
	}
	return b.String(), help
}

func (m *Model) viewJobDetail(j api.JobStatus) string {
	w := m.w - 4
	lbl := func(s string) string { return sMuted.Render(pad(s, 19)) }
	var lines []string

	title := sBold.Render(j.Job.Name)
	if !j.Job.Enabled {
		title += "  " + sMuted.Render("("+T("paused")+")")
	}
	lines = append(lines, title)
	ro := ""
	if j.Job.SourceRO {
		ro = "  " + sROBadge.Render(T("READ-ONLY"))
	}
	lines = append(lines, lbl(T("From"))+trunc(j.Source, w-36)+ro)
	lines = append(lines, lbl(T("To"))+trunc(j.Dest, w-20))
	mode := config.ModeLabel(j.Job.Mode)
	if j.Job.Mode == config.ModeMirrorArchive {
		if j.Job.ArchiveDays > 0 {
			mode += " " + Tf("(keeps %d days)", j.Job.ArchiveDays)
		} else {
			mode += " " + T("(keeps forever)")
		}
	}
	lines = append(lines, lbl(T("Mode"))+mode)
	sched := j.Job.Schedule.Describe()
	if !j.Job.Enabled {
		sched += sMuted.Render("  – " + T("paused, manual start only"))
	} else if !j.Next.IsZero() {
		sched += sMuted.Render("  – "+T("next:")+" "+fmtTime(j.Next)+" ") + sRun.Render("("+fmtUntil(j.Next.Sub(m.serverNow()))+")")
	}
	lines = append(lines, lbl(T("Schedule"))+sched)

	if r := j.Current; r != nil {
		lines = append(lines, "")
		tag := T("Running")
		if r.DryRun {
			tag = T("Dry run in progress")
		}
		lines = append(lines, sRun.Render(tag)+sMuted.Render("  "+Tf("for %s · %s", fmtDur(m.serverNow().Sub(r.Start)), r.Phase)))
		if p := r.Progress; p != nil {
			barW := min(max(w-50, 10), 50)
			lines = append(lines, progressBar(p.Percent, barW)+fmt.Sprintf(" %3d%%  %s  %s  ETA %s",
				p.Percent, api.HumanBytes(p.Bytes), p.Speed, p.ETA))
			if p.Current != "" {
				lines = append(lines, sMuted.Render(Tf("%d items · ", p.Files))+trunc(p.Current, w-20))
			}
		}
	} else if r := j.Last; r != nil {
		lines = append(lines, "")
		kind := T("Last run")
		if r.DryRun {
			kind = T("Last dry run")
		}
		lines = append(lines, lbl(kind)+statusLabel(r.Status)+
			sMuted.Render("  "+Tf("%s · duration %s · %s", fmtTime(r.Start), fmtDur(r.Duration()), api.TriggerLabel(r.Trigger))))
		if r.Message != "" {
			st := sMuted
			if r.Status == api.StatusError {
				st = sErr
			} else if r.Status == api.StatusWarning {
				st = sWarn
			}
			// error messages can be long: wrap them (max 5 lines)
			msgW := max(w-22, 20)
			wrapped := strings.Split(lipgloss.NewStyle().Width(msgW).Render(strings.Join(strings.Fields(r.Message), " ")), "\n")
			if len(wrapped) > 5 {
				wrapped = append(wrapped[:4], trunc(strings.TrimSpace(wrapped[4])+" …", msgW))
			}
			for _, l := range wrapped {
				lines = append(lines, lbl("")+st.Render(strings.TrimRight(l, " ")))
			}
		}
	}
	return sBox.Width(m.w - 2).Render(strings.Join(lines, "\n"))
}

// ---------- scheda connessioni ----------

func (m *Model) viewConns() (string, string) {
	help := T("n new · enter edit · t test connection · d delete · tab next tab · L language · q quit")
	conns := m.conns()
	if len(conns) == 0 {
		return "\n  " + sBold.Render(T("No saved connections.")) + "\n\n" +
			"  " + T("A connection holds the address of the PC/server and the login credentials.") + "\n" +
			"  " + T("The password is encrypted (AES-256) and is never visible after saving.") + "\n\n" +
			"  " + Tf("Press %s to add one.", sKey.Render("n")) + "\n", T("n new · tab next tab · L language · q quit")
	}
	used := map[string]int{}
	for _, j := range m.jobs() {
		used[j.Job.Source.ConnectionID]++
		if j.Job.Dest.ConnectionID != j.Job.Source.ConnectionID {
			used[j.Job.Dest.ConnectionID]++
		}
	}
	nameW, hostW, userW, verW, pwW := 26, 22, 26, 10, 12
	var b strings.Builder
	b.WriteString(sMuted.Render("   "+pad(T("NAME"), nameW)+" "+pad(T("HOST"), hostW)+" "+pad(T("USER"), userW)+" "+
		pad("SMB", verW)+" "+pad(T("PASSWORD"), pwW)+" "+T("USED BY")) + "\n")
	for i, c := range conns {
		user := c.Username
		if c.Domain != "" {
			user = c.Domain + `\` + c.Username
		}
		ver := c.SMBVersion
		if ver == "" {
			ver = "auto"
		}
		pw := T("saved")
		if !c.HasPassword {
			pw = T("MISSING")
		}
		row := pad(c.Name, nameW) + " " + pad(c.Host, hostW) + " " + pad(user, userW) + " " +
			pad(ver, verW) + " " + pad(pw, pwW) + " " + Tf("%d jobs", used[c.ID])
		line := "   " + row
		if i == m.connCur {
			line = " " + sKey.Render(">") + " " + sSel.Render(row)
		}
		b.WriteString(zone.Mark(fmt.Sprintf("conn:%d", i), line) + "\n")
	}
	b.WriteString("\n" + sMuted.Render("  "+T("Passwords are encrypted in the configuration file with a master key readable only by root.")))
	return b.String(), help
}

// ---------- scheda storico ----------

func (m *Model) viewHistory() (string, string) {
	help := T("↑↓ scroll · enter open log · r refresh · tab next tab · L language · q quit")
	if len(m.history) == 0 {
		return "\n  " + sMuted.Render(T("No runs recorded.")), help
	}
	startW, jobW, durW, stW := 13, 24, 9, 14
	msgW := max(m.w-startW-jobW-durW-stW-10, 10)
	var b strings.Builder
	b.WriteString(sMuted.Render("   "+pad(T("START"), startW)+" "+pad(T("JOB"), jobW)+" "+pad(T("DURATION"), durW)+" "+
		pad(T("RESULT"), stW)+" "+T("DETAILS")) + "\n")
	listH := max(m.h-8-m.warningsHeight()-helpExtra(help, m.w), 3)
	m.histOffset = listWindow(m.histCur, m.histOffset, len(m.history), listH)
	for i := m.histOffset; i < len(m.history) && i < m.histOffset+listH; i++ {
		r := m.history[i]
		job := r.JobName
		if r.DryRun {
			job += " (sim)"
		}
		stTxt := statusText(r.Status)
		head := pad(fmtTime(r.Start), startW) + " " + pad(job, jobW) + " " + pad(fmtDur(r.Duration()), durW) + " "
		tail := " " + trunc(r.Message, msgW)
		row := head + pad(stTxt, stW) + tail
		colored := head + statusStyle(r.Status).Render(pad(stTxt, stW)) + tail
		line := "   " + colored
		if i == m.histCur {
			line = " " + sKey.Render(">") + " " + sSel.Render(row)
		}
		b.WriteString(zone.Mark(fmt.Sprintf("hist:%d", i), line) + "\n")
	}
	return b.String(), help
}

// ---------- log ----------

func (m *Model) viewLog() string {
	help := renderHelp(T("↑↓ scroll · PgUp/PgDn page · g/G top/bottom · r reload · esc close"), m.w)
	m.logView.Height = max(m.h-3-lipgloss.Height(help), 3)
	title := sTitle.Render(" Log: ") + sBold.Render(m.logTitle)
	pct := fmt.Sprintf("%3.0f%%", m.logView.ScrollPercent()*100)
	head := title + strings.Repeat(" ", max(m.w-lipgloss.Width(title)-len(pct)-1, 1)) + sMuted.Render(pct)
	return head + "\n" + rule(m.w) + "\n" + m.logView.View() + "\n" + help
}

// ---------- form ----------

func (m *Model) viewForm() (string, string) {
	fm := m.form
	help := T("↑↓ field · ←→ choose · space toggle · ctrl+s save · esc cancel")
	var top string
	if m.formKind == "job" {
		top = m.jobPreview() + "\n"
	}
	head := " " + sTitle.Render(fm.Title) + "\n"
	avail := m.h - 6 - formDescHeight - lipgloss.Height(head) - lipgloss.Height(top) - helpExtra(help, m.w)
	body := fm.render(m.w-2, avail)
	return head + top + lipgloss.NewStyle().PaddingLeft(1).Render(body), help
}

// formDescHeight is the fixed height of the description bar at the bottom of the form.
const formDescHeight = 3

// viewFormDescription is the fixed bar at the bottom of the form: it describes the
// active field or shows the validation error. It always has the same height.
func (m *Model) viewFormDescription() string {
	fm := m.form
	w := m.w - 2
	var text string
	switch {
	case fm.Err != "":
		st := sErr
		if strings.HasSuffix(fm.Err, "…") {
			st = sMuted
		}
		text = st.Render(fm.Err)
	case m.saving:
		text = sMuted.Render(T("saving…"))
	default:
		label, desc := fm.description()
		if desc == "" {
			desc = "—"
		}
		text = sBold.Render(label+": ") + sMuted.Render(desc)
	}
	lines := strings.Split(lipgloss.NewStyle().Width(w).Render(text), "\n")
	if len(lines) > formDescHeight-1 {
		lines = lines[:formDescHeight-1]
	}
	for len(lines) < formDescHeight-1 {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = " " + lines[i]
	}
	return rule(m.w) + "\n" + strings.Join(lines, "\n")
}

// jobPreview shows source → destination visually while the form is being filled in.
func (m *Model) jobPreview() string {
	fm := m.form
	cfg := &config.Config{}
	for _, c := range m.conns() {
		cfg.Connections = append(cfg.Connections, c.Connection)
	}
	loc := func(p string) config.Location {
		l := config.Location{Type: fm.choice(p + "_type"), Path: fm.val(p + "_path")}
		if l.Type == config.LocSMB {
			l.ConnectionID, l.Share = fm.choice(p+"_conn"), fm.val(p+"_share")
			l.Path = strings.Trim(strings.ReplaceAll(l.Path, `\`, "/"), "/")
		}
		return l
	}
	show := func(l config.Location) string {
		if l.Type == config.LocSMB && l.Share == "" {
			return sMuted.Render(T("(choose the share)"))
		}
		if l.Type == config.LocLocal && l.Path == "" {
			return sMuted.Render(T("(choose the folder)"))
		}
		return l.Display(cfg)
	}
	boxW := max((m.w-10)/2, 20)
	inner := boxW - 4
	src := sMuted.Render(T("SOURCE")) + "\n" + truncLeft(show(loc("src")), inner) + "\n"
	if fm.get("src_ro").Bool {
		src += sROBadge.Render(T("READ-ONLY"))
	} else {
		src += sErr.Render(T("WARNING: writable source"))
	}
	dst := sMuted.Render(T("DESTINATION")) + "\n" + truncLeft(show(loc("dst")), inner) + "\n" +
		sMuted.Render(trunc(config.ModeLabel(fm.choice("mode")), inner))
	arrow := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Padding(0, 1).Render("\n──>")
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		sFocusBox.Width(boxW).Render(src), arrow, sBox.Width(boxW).Render(dst))
	desc := sMuted.Render("  " + trunc(config.ModeDescription(fm.choice("mode")), m.w-4))
	return lipgloss.NewStyle().PaddingLeft(1).Render(row) + "\n" + desc
}

// ---------- finestre modali ----------

func (m *Model) overlay() string {
	modalW := min(max(m.w-10, 40), 90)
	if _, pending := m.fwPendingLeft(); pending && m.form == nil {
		return m.viewFwConfirm(modalW)
	}
	switch {
	case m.info != nil:
		return sFocusBox.Width(modalW).Render(sTitle.Render(m.info.title) + "\n\n" + m.info.body +
			"\n\n" + renderHelp(T("enter close"), modalW))
	case m.confirm != nil:
		return sFocusBox.Width(min(modalW, 70)).Render(sBold.Render(m.confirm.text) + "\n\n" +
			renderHelp(T("y yes · n no"), modalW))
	case m.picker != nil:
		p := m.picker
		var lines []string
		h := max(m.h-10, 3)
		off := listWindow(p.cur, 0, len(p.items), h)
		for i := off; i < len(p.items) && i < off+h; i++ {
			line := pad(" "+p.items[i], modalW-4)
			if i == p.cur {
				line = sSel.Render(line)
			}
			lines = append(lines, zone.Mark(fmt.Sprintf("pick:%d", i), line))
		}
		return sFocusBox.Width(modalW).Render(sTitle.Render(p.title) + "\n\n" + strings.Join(lines, "\n") +
			"\n\n" + renderHelp(T("enter choose · esc cancel"), modalW))
	case m.browser != nil:
		return m.viewBrowser(modalW)
	}
	return ""
}

func (m *Model) viewBrowser(w int) string {
	b := m.browser
	title := T("Choose folder")
	if b.loc.Type == config.LocSMB {
		host := ""
		for _, c := range m.conns() {
			if c.ID == b.loc.ConnectionID {
				host = c.Host
			}
		}
		title += sMuted.Render("  " + T("on") + " " + `\\` + host)
	}
	path := sBold.Render(truncLeft(b.displayPath(), w-6))
	var body string
	switch {
	case b.loading:
		body = sMuted.Render(" " + T("reading…"))
	default:
		items := b.items()
		h := max(m.h-14, 3)
		b.offset = listWindow(b.cur, b.offset, len(items), h)
		var lines []string
		for i := b.offset; i < len(items) && i < b.offset+h; i++ {
			it := items[i]
			txt := pad(" "+it, w-4)
			switch {
			case i == b.cur:
				txt = sSel.Render(txt)
			case i == 0:
				txt = sOK.Render(txt)
			case i == b.newIndex():
				txt = sKey.Render(txt)
			case strings.HasPrefix(it, ".."):
				txt = sMuted.Render(txt)
			}
			lines = append(lines, zone.Mark(fmt.Sprintf("br:%d", i), txt))
		}
		body = strings.Join(lines, "\n")
		if b.naming {
			body += "\n\n " + sBold.Render(T("New folder name:")) + " " + b.input.View()
		}
		if b.err != "" {
			body += "\n\n" + sErr.Render(trunc(" "+b.err, w-4))
		} else if len(b.dirs) == 0 {
			body += "\n" + sMuted.Render(" ("+T("no subfolders")+")")
		}
	}
	help := T("enter open/select · ← up one level · s use current folder · esc cancel")
	switch {
	case b.naming:
		help = T("enter create the folder · esc cancel")
	case b.allowNew:
		help = T("enter open/select · ← up one level · s use current folder · n new folder · esc cancel")
	}
	return sFocusBox.Width(w).Render(sTitle.Render(title) + "\n" + path + "\n\n" + body + "\n\n" +
		renderHelp(help, w))
}

// fmtClock formats the server time, e.g. "Mon 2026-10-05 14:32:07 CEST".
func fmtClock(t time.Time, zone string) string {
	s := config.DayName(int(t.Weekday())) + " " + t.Format(i18n.DateTimeLayout())
	if zone != "" {
		s += " " + zone
	}
	return s
}

// fmtUntil describes how long until a run, e.g. "in 2h 15m".
func fmtUntil(d time.Duration) string {
	switch {
	case d < time.Minute:
		return T("in less than a minute")
	case d < time.Hour:
		return Tf("in %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return Tf("in %dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	return Tf("in %d d %dh", days, int(d.Hours())%24)
}

// viewLanguages is the language selector next to the clock (clickable, or key L).
func (m *Model) viewLanguages() string {
	var parts []string
	for _, l := range i18n.Languages {
		st := sTabOff.Padding(0)
		if l.Code == i18n.Lang() {
			st = sTabOn.Padding(0, 0)
		}
		parts = append(parts, zone.Mark("lang:"+l.Code, st.Render(strings.ToUpper(l.Code))))
	}
	return strings.Join(parts, sSep.Render("|"))
}
