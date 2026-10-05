package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

func (m *Model) View() string {
	if m.w == 0 {
		return "caricamento…"
	}
	var body, help string
	switch {
	case m.logOpen:
		return m.viewLog()
	case m.form != nil:
		body, help = m.viewForm()
	case m.st == nil && m.connErr != nil:
		body, help = m.viewNoDaemon(), "q esci"
	case m.st == nil:
		body = sMuted.Render("  connessione al servizio…")
	default:
		switch m.tab {
		case tabJobs:
			body, help = m.viewJobs()
		case tabConns:
			body, help = m.viewConns()
		case tabHistory:
			body, help = m.viewHistory()
		}
	}

	header := m.viewHeader()
	footer := m.viewFooter(help)
	bodyH := m.h - lipgloss.Height(header) - lipgloss.Height(footer)
	body = fitHeight(body, bodyH)

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

// placeOverlay centra una finestra modale nello schermo.
func placeOverlay(w, h int, box string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceChars(" "))
}

func (m *Model) viewHeader() string {
	left := sTitle.Render(" VegaSyncor ")
	var tabs []string
	for i, n := range tabNames {
		if tab(i) == m.tab && m.form == nil {
			tabs = append(tabs, sTabOn.Render(n))
		} else {
			tabs = append(tabs, sTabOff.Render(n))
		}
	}
	// parti a destra in ordine di importanza: l'orologio resta sempre, poi stato e nome host
	var state, clock, clockShort, host string
	switch {
	case m.connErr != nil:
		state = sErr.Render("servizio non raggiungibile")
	case m.st != nil:
		running := 0
		for _, j := range m.st.Jobs {
			if j.Current != nil {
				running++
			}
		}
		state = sOK.Render("servizio attivo")
		if running > 0 {
			state = sRun.Render(fmt.Sprintf("%d in esecuzione", running))
		}
		now := m.serverNow()
		clock = sClock.Render(fmtClock(now, m.st.ZoneAbbr))
		clockShort = sClock.Render(now.Format("02/01 15:04:05"))
		host = sMuted.Render(m.st.Hostname)
	}
	line := left + " " + strings.Join(tabs, " ")
	var right string
	for _, parts := range [][]string{{state, clock, host}, {state, clock}, {clock}, {clockShort}} {
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

// viewWarnings mostra in evidenza i problemi dell'ambiente segnalati dal servizio
// (es. container non privilegiato), con il testo completo a capo.
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
			msg = sErr.Render(" ERRORE: " + trunc(m.flash, m.w-10))
		} else {
			msg = sOK.Render(" OK: " + trunc(m.flash, m.w-6))
		}
	} else if m.connErr != nil && m.st != nil {
		msg = sErr.Render(" ERRORE: " + trunc(m.connErr.Error(), m.w-10))
	}
	return msg + "\n" + rule(m.w) + "\n" + renderHelp(help, m.w)
}

// renderHelp evidenzia i tasti: formato "tasto descrizione · tasto descrizione".
func renderHelp(h string, w int) string {
	if h == "" {
		return ""
	}
	var parts []string
	for _, p := range strings.Split(h, " · ") {
		k, d, _ := strings.Cut(p, " ")
		parts = append(parts, sKey.Render(k)+" "+sMuted.Render(d))
	}
	out := " " + strings.Join(parts, sMuted.Render("  ·  "))
	if lipgloss.Width(out) > w {
		// riduce i separatori se lo spazio è poco
		out = " " + strings.Join(parts, " ")
	}
	return out
}

func (m *Model) viewNoDaemon() string {
	msg := sErr.Render("Impossibile contattare il servizio VegaSyncor") + "\n\n" +
		m.connErr.Error() + "\n\n" +
		sMuted.Render("Verificare che sia avviato:") + "\n" +
		"  sudo systemctl status vegasyncor\n  sudo systemctl start vegasyncor\n\n" +
		sMuted.Render("La TUI riprova automaticamente ogni secondo.")
	return "\n" + lipgloss.NewStyle().MarginLeft(2).Render(sBox.Render(msg))
}

// ---------- scheda sincronizzazioni ----------

func scheduleText(j api.JobStatus) string {
	if !j.Job.Enabled {
		return "sospeso"
	}
	return j.Job.Schedule.Describe()
}

func (m *Model) viewJobs() (string, string) {
	help := "n nuova · invio modifica · r avvia · s simula · x interrompi · p sospendi · l log · d elimina · tab scheda · q esci"
	jobs := m.jobs()
	if len(jobs) == 0 {
		var b strings.Builder
		b.WriteString("\n  " + sBold.Render("Nessuna sincronizzazione configurata.") + "\n\n")
		if len(m.conns()) == 0 {
			b.WriteString("  Per iniziare:\n")
			b.WriteString("    1. vai alla scheda " + sKey.Render("2 Connessioni") + " e premi " + sKey.Render("n") + " per salvare le credenziali di un PC o server\n")
			b.WriteString("    2. torna qui e premi " + sKey.Render("n") + " per creare la prima sincronizzazione\n")
		} else {
			b.WriteString("  Premi " + sKey.Render("n") + " per creare la prima sincronizzazione.\n")
		}
		return b.String(), "n nuova · tab scheda · q esci"
	}

	w := m.w
	// colonne: icona(2) nome quando ultima prossima + percorso (resto)
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
	hdr := "       " + pad("NOME", nameW) + " " + pad("SORGENTE  →  DESTINAZIONE", routeW) + " "
	if showWhen {
		hdr += pad("QUANDO", whenW) + " "
	}
	hdr += pad("ULTIMA", lastW) + " " + pad("PROSSIMA", nextW)
	b.WriteString(sMuted.Render(hdr) + "\n")

	listH := max(m.h-22-m.warningsHeight(), 3)
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
		last := "mai eseguito"
		if j.Current != nil {
			last = "in corso"
			if p := j.Current.Progress; p != nil {
				last = fmt.Sprintf("in corso %d%%", p.Percent)
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
			b.WriteString(" " + icon + " " + sSel.Render(row) + "\n")
		} else {
			b.WriteString(" " + icon + " " + row + "\n")
		}
	}
	if sel := m.selJob(); sel != nil {
		b.WriteString("\n" + m.viewJobDetail(*sel))
	}
	return b.String(), help
}

func (m *Model) viewJobDetail(j api.JobStatus) string {
	w := m.w - 4
	lbl := func(s string) string { return sMuted.Render(pad(s, 18)) }
	var lines []string

	title := sBold.Render(j.Job.Name)
	if !j.Job.Enabled {
		title += "  " + sMuted.Render("(sospeso)")
	}
	lines = append(lines, title)
	ro := ""
	if j.Job.SourceRO {
		ro = "  " + sROBadge.Render("SOLA LETTURA")
	}
	lines = append(lines, lbl("Da")+trunc(j.Source, w-36)+ro)
	lines = append(lines, lbl("A")+trunc(j.Dest, w-20))
	mode := config.ModeLabel(j.Job.Mode)
	if j.Job.Mode == config.ModeMirrorArchive {
		if j.Job.ArchiveDays > 0 {
			mode += fmt.Sprintf(" (conserva %d gg)", j.Job.ArchiveDays)
		} else {
			mode += " (conserva per sempre)"
		}
	}
	lines = append(lines, lbl("Modalità")+mode)
	sched := j.Job.Schedule.Describe()
	if !j.Job.Enabled {
		sched += sMuted.Render("  – sospeso, solo avvio manuale")
	} else if !j.Next.IsZero() {
		sched += sMuted.Render("  – prossima: "+fmtTime(j.Next)+" ") + sRun.Render("("+fmtUntil(j.Next.Sub(m.serverNow()))+")")
	}
	lines = append(lines, lbl("Pianificazione")+sched)

	if r := j.Current; r != nil {
		lines = append(lines, "")
		tag := "In esecuzione"
		if r.DryRun {
			tag = "Simulazione in corso"
		}
		lines = append(lines, sRun.Render(tag)+sMuted.Render(fmt.Sprintf("  da %s · %s", fmtDur(m.serverNow().Sub(r.Start)), r.Phase)))
		if p := r.Progress; p != nil {
			barW := min(max(w-50, 10), 50)
			lines = append(lines, progressBar(p.Percent, barW)+fmt.Sprintf(" %3d%%  %s  %s  ETA %s",
				p.Percent, api.HumanBytes(p.Bytes), p.Speed, p.ETA))
			if p.Current != "" {
				lines = append(lines, sMuted.Render(fmt.Sprintf("%d elementi · ", p.Files))+trunc(p.Current, w-20))
			}
		}
	} else if r := j.Last; r != nil {
		lines = append(lines, "")
		kind := "Ultima esecuzione"
		if r.DryRun {
			kind = "Ultima simulazione"
		}
		lines = append(lines, lbl(kind)+statusLabel(r.Status)+
			sMuted.Render(fmt.Sprintf("  %s · durata %s · %s", fmtTime(r.Start), fmtDur(r.Duration()), r.Trigger)))
		if r.Message != "" {
			st := sMuted
			if r.Status == api.StatusError {
				st = sErr
			} else if r.Status == api.StatusWarning {
				st = sWarn
			}
			// i messaggi di errore possono essere lunghi: vanno a capo (max 5 righe)
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
	help := "n nuova · invio modifica · t prova connessione · d elimina · tab scheda · q esci"
	conns := m.conns()
	if len(conns) == 0 {
		return "\n  " + sBold.Render("Nessuna connessione salvata.") + "\n\n" +
			"  Una connessione contiene indirizzo del PC/server e credenziali di accesso.\n" +
			"  La password viene cifrata (AES-256) e non è mai visibile dopo il salvataggio.\n\n" +
			"  Premi " + sKey.Render("n") + " per aggiungerne una.\n", "n nuova · tab scheda · q esci"
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
	b.WriteString(sMuted.Render("   "+pad("NOME", nameW)+" "+pad("HOST", hostW)+" "+pad("UTENTE", userW)+" "+
		pad("SMB", verW)+" "+pad("PASSWORD", pwW)+" USATA DA") + "\n")
	for i, c := range conns {
		user := c.Username
		if c.Domain != "" {
			user = c.Domain + `\` + c.Username
		}
		ver := c.SMBVersion
		if ver == "" {
			ver = "auto"
		}
		pw := "salvata"
		if !c.HasPassword {
			pw = "MANCANTE"
		}
		row := pad(c.Name, nameW) + " " + pad(c.Host, hostW) + " " + pad(user, userW) + " " +
			pad(ver, verW) + " " + pad(pw, pwW) + " " + fmt.Sprintf("%d job", used[c.ID])
		if i == m.connCur {
			b.WriteString(" " + sKey.Render(">") + " " + sSel.Render(row) + "\n")
		} else {
			b.WriteString("   " + row + "\n")
		}
	}
	b.WriteString("\n" + sMuted.Render("  Le password sono cifrate nel file di configurazione con una chiave master accessibile solo a root."))
	return b.String(), help
}

// ---------- scheda storico ----------

func (m *Model) viewHistory() (string, string) {
	help := "↑↓ scorri · invio apri log · r aggiorna · tab scheda · q esci"
	if len(m.history) == 0 {
		return "\n  " + sMuted.Render("Nessuna esecuzione registrata."), help
	}
	startW, jobW, durW, stW := 13, 24, 8, 12
	msgW := max(m.w-startW-jobW-durW-stW-10, 10)
	var b strings.Builder
	b.WriteString(sMuted.Render("   "+pad("AVVIO", startW)+" "+pad("JOB", jobW)+" "+pad("DURATA", durW)+" "+
		pad("ESITO", stW)+" DETTAGLI") + "\n")
	listH := max(m.h-8-m.warningsHeight(), 3)
	m.histOffset = listWindow(m.histCur, m.histOffset, len(m.history), listH)
	for i := m.histOffset; i < len(m.history) && i < m.histOffset+listH; i++ {
		r := m.history[i]
		job := r.JobName
		if r.DryRun {
			job += " (sim)"
		}
		stTxt := map[string]string{api.StatusOK: "completato", api.StatusWarning: "avvisi", api.StatusError: "errore",
			api.StatusCancelled: "annullato", api.StatusSkipped: "saltato", api.StatusRunning: "in corso"}[r.Status]
		head := pad(fmtTime(r.Start), startW) + " " + pad(job, jobW) + " " + pad(fmtDur(r.Duration()), durW) + " "
		tail := " " + trunc(r.Message, msgW)
		row := head + pad(stTxt, stW) + tail
		colored := head + statusStyle(r.Status).Render(pad(stTxt, stW)) + tail
		if i == m.histCur {
			b.WriteString(" " + sKey.Render(">") + " " + sSel.Render(row) + "\n")
		} else {
			b.WriteString("   " + colored + "\n")
		}
	}
	return b.String(), help
}

// ---------- log ----------

func (m *Model) viewLog() string {
	title := sTitle.Render(" Log: ") + sBold.Render(m.logTitle)
	pct := fmt.Sprintf("%3.0f%%", m.logView.ScrollPercent()*100)
	head := title + strings.Repeat(" ", max(m.w-lipgloss.Width(title)-len(pct)-1, 1)) + sMuted.Render(pct)
	return head + "\n" + rule(m.w) + "\n" + m.logView.View() + "\n" +
		renderHelp("↑↓ scorri · PgSu/PgGiù pagina · g/G inizio/fine · r ricarica · esc chiudi", m.w)
}

// ---------- form ----------

func (m *Model) viewForm() (string, string) {
	fm := m.form
	help := "↑↓ campo · ←→ scegli · spazio attiva · ctrl+s salva · esc annulla"
	var top string
	if m.formKind == "job" {
		top = m.jobPreview() + "\n"
	}
	errLine := ""
	if fm.Err != "" {
		st := sErr
		if strings.HasSuffix(fm.Err, "…") {
			st = sMuted
		}
		errLine = "\n" + st.Render(" "+fm.Err)
	} else if m.saving {
		errLine = "\n" + sMuted.Render(" salvataggio…")
	}
	head := " " + sTitle.Render(fm.Title) + "\n"
	avail := m.h - 6 - lipgloss.Height(head) - lipgloss.Height(top) - lipgloss.Height(errLine)
	body := fm.render(m.w-2, avail)
	return head + top + lipgloss.NewStyle().PaddingLeft(1).Render(body) + errLine, help
}

// jobPreview mostra in modo visivo sorgente → destinazione mentre si compila il form.
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
			return sMuted.Render("(scegliere la condivisione)")
		}
		if l.Type == config.LocLocal && l.Path == "" {
			return sMuted.Render("(scegliere la cartella)")
		}
		return l.Display(cfg)
	}
	boxW := max((m.w-10)/2, 20)
	inner := boxW - 4
	src := sMuted.Render("SORGENTE") + "\n" + truncLeft(show(loc("src")), inner) + "\n"
	if fm.get("src_ro").Bool {
		src += sROBadge.Render("SOLA LETTURA")
	} else {
		src += sErr.Render("ATTENZIONE: sorgente scrivibile")
	}
	dst := sMuted.Render("DESTINAZIONE") + "\n" + truncLeft(show(loc("dst")), inner) + "\n" +
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
	switch {
	case m.info != nil:
		return sFocusBox.Width(modalW).Render(sTitle.Render(m.info.title) + "\n\n" + m.info.body +
			"\n\n" + renderHelp("invio chiudi", modalW))
	case m.confirm != nil:
		return sFocusBox.Width(min(modalW, 70)).Render(sBold.Render(m.confirm.text) + "\n\n" +
			renderHelp("s sì · n no", modalW))
	case m.picker != nil:
		p := m.picker
		var lines []string
		h := max(m.h-10, 3)
		off := listWindow(p.cur, 0, len(p.items), h)
		for i := off; i < len(p.items) && i < off+h; i++ {
			if i == p.cur {
				lines = append(lines, sSel.Render(pad(" "+p.items[i], modalW-4)))
			} else {
				lines = append(lines, " "+p.items[i])
			}
		}
		return sFocusBox.Width(modalW).Render(sTitle.Render(p.title) + "\n\n" + strings.Join(lines, "\n") +
			"\n\n" + renderHelp("invio scegli · esc annulla", modalW))
	case m.browser != nil:
		return m.viewBrowser(modalW)
	}
	return ""
}

func (m *Model) viewBrowser(w int) string {
	b := m.browser
	title := "Scegli cartella"
	if b.loc.Type == config.LocSMB {
		host := ""
		for _, c := range m.conns() {
			if c.ID == b.loc.ConnectionID {
				host = c.Host
			}
		}
		title += sMuted.Render("  su " + `\\` + host)
	}
	path := sBold.Render(truncLeft(b.displayPath(), w-6))
	var body string
	switch {
	case b.loading:
		body = sMuted.Render(" lettura in corso…")
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
			case strings.HasPrefix(it, ".."):
				txt = sMuted.Render(txt)
			}
			lines = append(lines, txt)
		}
		body = strings.Join(lines, "\n")
		if b.err != "" {
			body += "\n\n" + sErr.Render(trunc(" "+b.err, w-4))
		} else if len(b.dirs) == 0 {
			body += "\n" + sMuted.Render(" (nessuna sottocartella)")
		}
	}
	return sFocusBox.Width(w).Render(sTitle.Render(title) + "\n" + path + "\n\n" + body + "\n\n" +
		renderHelp("invio apri/seleziona · ← su di un livello · s usa cartella corrente · esc annulla", w))
}

var weekdays = []string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"}

// fmtClock formatta l'ora del server, es. "lun 05/10/2026 14:32:07 CEST".
func fmtClock(t time.Time, zone string) string {
	s := weekdays[t.Weekday()] + " " + t.Format("02/01/2006 15:04:05")
	if zone != "" {
		s += " " + zone
	}
	return s
}

// fmtUntil descrive quanto manca a un'esecuzione, es. "tra 2h 15m".
func fmtUntil(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "tra meno di un minuto"
	case d < time.Hour:
		return fmt.Sprintf("tra %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("tra %dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	return fmt.Sprintf("tra %d g %dh", days, int(d.Hours())%24)
}
