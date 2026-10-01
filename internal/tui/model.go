// Package tui implementa l'interfaccia testuale di gestione (utilizzabile via SSH).
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
)

type tab int

const (
	tabJobs tab = iota
	tabConns
	tabHistory
)

var tabNames = []string{"1 Sincronizzazioni", "2 Connessioni", "3 Storico"}

type confirmBox struct {
	text   string
	action tea.Cmd
}

type infoBox struct {
	title string
	body  string
}

type Model struct {
	client  *api.Client
	version string
	w, h    int

	tab        tab
	st         *api.Status
	connErr    error
	polling    bool
	jobCur     int
	connCur    int
	histCur    int
	histOffset int
	history    []api.Run

	flash    string
	flashErr bool
	flashAt  time.Time

	form     *form
	formKind string // "job" | "conn"
	formID   string
	saving   bool

	browser *browser
	picker  *picker
	confirm *confirmBox
	info    *infoBox

	logOpen  bool
	logRun   string
	logTitle string
	logView  viewport.Model
}

// ---------- messaggi ----------

type tickMsg time.Time
type statusMsg struct {
	st  *api.Status
	err error
}
type historyMsg struct {
	runs []api.Run
	err  error
}
type actionMsg struct {
	ok  string
	err error
}
type savedMsg struct{ err error }
type logMsg struct {
	run, title, text string
	err              error
}
type browseMsg struct {
	resp *api.BrowseResponse
	err  error
}
type sharesMsg struct {
	target string
	res    *api.TestResult
	err    error
}
type testMsg struct {
	name string
	res  *api.TestResult
	err  error
}

func Run(c *api.Client, version string) error {
	m := &Model{client: c, version: version}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) fetchStatus() tea.Cmd {
	m.polling = true
	c := m.client
	return func() tea.Msg {
		st, err := c.Status()
		return statusMsg{st, err}
	}
}

func (m *Model) fetchHistory() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		runs, err := c.History("", 300)
		return historyMsg{runs, err}
	}
}

func (m *Model) do(ok string, fn func() error) tea.Cmd {
	return func() tea.Msg { return actionMsg{ok: ok, err: fn()} }
}

func (m *Model) openLog(runID, title string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		text, err := c.RunLog(runID, 2<<20)
		return logMsg{run: runID, title: title, text: text, err: err}
	}
}

func (m *Model) setFlash(s string, isErr bool) {
	m.flash, m.flashErr, m.flashAt = s, isErr, time.Now()
}

// ---------- selezione ----------

func (m *Model) jobs() []api.JobStatus {
	if m.st == nil {
		return nil
	}
	return m.st.Jobs
}

func (m *Model) conns() []api.ConnectionView {
	if m.st == nil {
		return nil
	}
	return m.st.Connections
}

func (m *Model) selJob() *api.JobStatus {
	js := m.jobs()
	if m.jobCur < 0 || m.jobCur >= len(js) {
		return nil
	}
	return &js[m.jobCur]
}

func (m *Model) selConn() *api.ConnectionView {
	cs := m.conns()
	if m.connCur < 0 || m.connCur >= len(cs) {
		return nil
	}
	return &cs[m.connCur]
}

func clamp(v, n int) int {
	if v >= n {
		v = n - 1
	}
	if v < 0 {
		v = 0
	}
	return v
}

// ---------- update ----------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.logView.Width, m.logView.Height = m.w, max(m.h-4, 3)
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if !m.polling {
			cmds = append(cmds, m.fetchStatus())
		}
		if m.tab == tabHistory && time.Time(msg).Second()%5 == 0 {
			cmds = append(cmds, m.fetchHistory())
		}
		if m.flash != "" && time.Since(m.flashAt) > 6*time.Second {
			m.flash = ""
		}
		return m, tea.Batch(cmds...)

	case statusMsg:
		m.polling = false
		m.connErr = msg.err
		if msg.err == nil {
			m.st = msg.st
			m.jobCur = clamp(m.jobCur, len(m.st.Jobs))
			m.connCur = clamp(m.connCur, len(m.st.Connections))
		}
		return m, nil

	case historyMsg:
		if msg.err == nil {
			m.history = msg.runs
			m.histCur = clamp(m.histCur, len(m.history))
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error(), true)
		} else if msg.ok != "" {
			m.setFlash(msg.ok, false)
		}
		return m, m.fetchStatus()

	case savedMsg:
		m.saving = false
		if msg.err != nil {
			if m.form != nil {
				m.form.Err = msg.err.Error()
			}
			return m, nil
		}
		m.form = nil
		m.setFlash("Salvato.", false)
		return m, m.fetchStatus()

	case logMsg:
		if msg.err != nil {
			m.setFlash("Log: "+msg.err.Error(), true)
			return m, nil
		}
		m.logOpen, m.logRun, m.logTitle = true, msg.run, msg.title
		m.logView = viewport.New(m.w, max(m.h-4, 3))
		m.logView.SetContent(colorizeLog(msg.text))
		m.logView.GotoBottom()
		return m, nil

	case browseMsg:
		if m.browser == nil {
			return m, nil
		}
		m.browser.loading = false
		if msg.err != nil {
			m.browser.err = msg.err.Error()
			m.browser.dirs = nil
		} else {
			m.browser.err = ""
			m.browser.dirs = msg.resp.Dirs
		}
		m.browser.cur, m.browser.offset = 0, 0
		return m, nil

	case sharesMsg:
		if m.form == nil {
			return m, nil
		}
		if msg.err != nil {
			m.form.Err = msg.err.Error()
		} else if !msg.res.OK {
			m.form.Err = msg.res.Message
		} else if len(msg.res.Shares) == 0 {
			m.form.Err = "nessuna condivisione trovata: inserire il nome a mano"
		} else {
			m.form.Err = ""
			m.picker = &picker{title: "Condivisioni disponibili", target: msg.target, items: msg.res.Shares}
		}
		return m, nil

	case testMsg:
		body := ""
		switch {
		case msg.err != nil:
			body = sErr.Render("✖ " + msg.err.Error())
		case !msg.res.OK:
			body = sErr.Render("✖ " + msg.res.Message)
			if !strings.Contains(msg.res.Message, "non installato") {
				body += "\n\n" + sMuted.Render("Controllare host, utente, password e dominio.")
			}
		default:
			body = sOK.Render("✔ "+msg.res.Message) + "\n"
			for _, s := range msg.res.Shares {
				body += "\n  • " + s
			}
		}
		m.info = &infoBox{title: "Test connessione: " + msg.name, body: body}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	switch {
	case m.info != nil:
		if key == "esc" || key == "enter" || key == "q" {
			m.info = nil
		}
		return nil
	case m.confirm != nil:
		switch key {
		case "s", "S", "y", "Y":
			cmd := m.confirm.action
			m.confirm = nil
			return cmd
		case "n", "N", "esc", "q":
			m.confirm = nil
		}
		return nil
	case m.picker != nil:
		return m.pickerKey(key)
	case m.browser != nil:
		return m.browserKey(key)
	case m.form != nil:
		return m.formKey(k)
	case m.logOpen:
		switch key {
		case "esc", "q", "l":
			m.logOpen = false
			return nil
		case "r":
			return m.openLog(m.logRun, m.logTitle)
		case "g", "home":
			m.logView.GotoTop()
			return nil
		case "G", "end":
			m.logView.GotoBottom()
			return nil
		}
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(k)
		return cmd
	}

	switch key {
	case "q":
		return tea.Quit
	case "1":
		m.tab = tabJobs
		return nil
	case "2":
		m.tab = tabConns
		return nil
	case "3", "h":
		m.tab = tabHistory
		return m.fetchHistory()
	case "tab":
		m.tab = (m.tab + 1) % 3
		if m.tab == tabHistory {
			return m.fetchHistory()
		}
		return nil
	case "shift+tab":
		m.tab = (m.tab + 2) % 3
		if m.tab == tabHistory {
			return m.fetchHistory()
		}
		return nil
	}
	if m.st == nil {
		return nil
	}
	switch m.tab {
	case tabJobs:
		return m.jobsKey(key)
	case tabConns:
		return m.connsKey(key)
	case tabHistory:
		return m.historyKey(key)
	}
	return nil
}

func (m *Model) jobsKey(key string) tea.Cmd {
	n := len(m.jobs())
	j := m.selJob()
	switch key {
	case "up", "k":
		m.jobCur = clamp(m.jobCur-1, n)
	case "down", "j":
		m.jobCur = clamp(m.jobCur+1, n)
	case "n", "a":
		m.form, m.formKind, m.formID = newJobForm(nil, m.conns()), "job", ""
	case "enter", "e":
		if j != nil {
			jj := j.Job
			m.form, m.formKind, m.formID = newJobForm(&jj, m.conns()), "job", j.Job.ID
		}
	case "r":
		if j != nil {
			id := j.Job.ID
			return m.do("Avviato: "+j.Job.Name, func() error { return m.client.RunJob(id, false) })
		}
	case "s":
		if j != nil {
			id := j.Job.ID
			return m.do("Simulazione avviata: "+j.Job.Name+" (premere l per vedere il risultato)", func() error { return m.client.RunJob(id, true) })
		}
	case "x":
		if j != nil && j.Current != nil {
			id, name := j.Job.ID, j.Job.Name
			m.confirm = &confirmBox{
				text:   fmt.Sprintf("Interrompere l'esecuzione di %q?", name),
				action: m.do("Annullamento richiesto", func() error { return m.client.CancelJob(id) }),
			}
		}
	case "p", " ":
		if j != nil {
			id, en := j.Job.ID, !j.Job.Enabled
			msg := "Job riattivato"
			if !en {
				msg = "Job sospeso (non partirà da pianificazione)"
			}
			return m.do(msg, func() error { return m.client.SetJobEnabled(id, en) })
		}
	case "d", "delete":
		if j != nil {
			id, name := j.Job.ID, j.Job.Name
			m.confirm = &confirmBox{
				text:   fmt.Sprintf("Eliminare la sincronizzazione %q?\nI file già copiati nella destinazione NON vengono toccati.", name),
				action: m.do("Eliminato: "+name, func() error { return m.client.DeleteJob(id) }),
			}
		}
	case "l":
		if j != nil {
			if j.Current != nil {
				return m.openLog(j.Current.ID, j.Job.Name+" – in corso")
			}
			if j.Last != nil {
				return m.openLog(j.Last.ID, j.Job.Name+" – "+fmtTime(j.Last.Start))
			}
			m.setFlash("Nessuna esecuzione per questo job", true)
		}
	}
	return nil
}

func (m *Model) connsKey(key string) tea.Cmd {
	n := len(m.conns())
	c := m.selConn()
	switch key {
	case "up", "k":
		m.connCur = clamp(m.connCur-1, n)
	case "down", "j":
		m.connCur = clamp(m.connCur+1, n)
	case "n", "a":
		m.form, m.formKind, m.formID = newConnForm(nil), "conn", ""
	case "enter", "e":
		if c != nil {
			cc := *c
			m.form, m.formKind, m.formID = newConnForm(&cc), "conn", c.ID
		}
	case "t":
		if c != nil {
			id, name := c.ID, c.Name
			m.setFlash("Test di "+name+" in corso…", false)
			return func() tea.Msg {
				res, err := m.client.TestConnection(id)
				return testMsg{name: name, res: res, err: err}
			}
		}
	case "d", "delete":
		if c != nil {
			id, name := c.ID, c.Name
			m.confirm = &confirmBox{
				text:   fmt.Sprintf("Eliminare la connessione %q e la password salvata?", name),
				action: m.do("Connessione eliminata", func() error { return m.client.DeleteConnection(id) }),
			}
		}
	}
	return nil
}

func (m *Model) historyKey(key string) tea.Cmd {
	n := len(m.history)
	switch key {
	case "up", "k":
		m.histCur = clamp(m.histCur-1, n)
	case "down", "j":
		m.histCur = clamp(m.histCur+1, n)
	case "pgup":
		m.histCur = clamp(m.histCur-10, n)
	case "pgdown":
		m.histCur = clamp(m.histCur+10, n)
	case "r":
		return m.fetchHistory()
	case "enter", "l":
		if m.histCur < n {
			r := m.history[m.histCur]
			return m.openLog(r.ID, r.JobName+" – "+fmtTime(r.Start))
		}
	}
	return nil
}

func (m *Model) formKey(k tea.KeyMsg) tea.Cmd {
	fm := m.form
	switch k.String() {
	case "esc":
		m.form = nil
		return nil
	case "ctrl+s", "f2":
		if m.saving {
			return nil
		}
		return m.saveForm()
	case "enter":
		if f := fm.current(); f.Browse {
			return m.startBrowse(f.Key)
		}
	}
	fm.Err = ""
	return fm.update(k)
}

func (m *Model) saveForm() tea.Cmd {
	fm, c := m.form, m.client
	switch m.formKind {
	case "job":
		j, err := jobFromForm(fm, m.formID)
		if err != nil {
			fm.Err = err.Error()
			return nil
		}
		if !j.SourceRO {
			// la sorgente scrivibile è consentita ma va confermata
			m.confirm = &confirmBox{
				text:   "La sorgente NON sarà protetta in sola lettura.\nConfermi il salvataggio?",
				action: func() tea.Msg { _, err := c.SaveJob(j); return savedMsg{err} },
			}
			return nil
		}
		m.saving = true
		return func() tea.Msg { _, err := c.SaveJob(j); return savedMsg{err} }
	case "conn":
		in := connFromForm(fm, m.formID)
		if m.formID == "" && in.Password == "" {
			fm.Err = "inserire la password"
			return nil
		}
		m.saving = true
		return func() tea.Msg { _, err := c.SaveConnection(in); return savedMsg{err} }
	}
	return nil
}

func (m *Model) startBrowse(key string) tea.Cmd {
	fm := m.form
	prefix := key[:3] // "src" | "dst"
	typ := fm.choice(prefix + "_type")
	if strings.HasSuffix(key, "_share") {
		conn := fm.choice(prefix + "_conn")
		if conn == "" {
			fm.Err = "creare prima una connessione"
			return nil
		}
		fm.Err = "lettura delle condivisioni…"
		c := m.client
		return func() tea.Msg {
			res, err := c.TestConnection(conn)
			return sharesMsg{target: key, res: res, err: err}
		}
	}
	loc := config.Location{Type: typ, Path: fm.val(key)}
	if typ == config.LocSMB {
		loc.ConnectionID = fm.choice(prefix + "_conn")
		loc.Share = fm.val(prefix + "_share")
		if loc.ConnectionID == "" || loc.Share == "" {
			fm.Err = "selezionare prima connessione e condivisione"
			return nil
		}
		loc.Path, _ = config.CleanSubPath(loc.Path)
	} else if loc.Path == "" {
		loc.Path = "/"
	}
	m.browser = &browser{target: key, loc: loc}
	return m.browse()
}

func (m *Model) browse() tea.Cmd {
	m.browser.loading = true
	loc, c := m.browser.loc, m.client
	return func() tea.Msg {
		resp, err := c.Browse(loc)
		return browseMsg{resp, err}
	}
}

func (m *Model) browserKey(key string) tea.Cmd {
	b := m.browser
	n := len(b.items())
	switch key {
	case "esc", "q":
		m.browser = nil
	case "up", "k":
		b.cur = clamp(b.cur-1, n)
	case "down", "j":
		b.cur = clamp(b.cur+1, n)
	case "pgup":
		b.cur = clamp(b.cur-10, n)
	case "pgdown":
		b.cur = clamp(b.cur+10, n)
	case "backspace", "left", "h":
		if !b.atRoot() && !b.loading {
			b.loc.Path = b.parent()
			return m.browse()
		}
	case "s":
		m.form.get(b.target).setValue(b.loc.Path)
		m.browser = nil
	case "enter", "right", "l":
		if b.loading {
			return nil
		}
		if b.cur == 0 {
			m.form.get(b.target).setValue(b.loc.Path)
			m.browser = nil
			return nil
		}
		if b.cur == 1 && !b.atRoot() {
			b.loc.Path = b.parent()
			return m.browse()
		}
		if p, ok := b.child(b.cur); ok {
			b.loc.Path = p
			return m.browse()
		}
	}
	return nil
}

func (m *Model) pickerKey(key string) tea.Cmd {
	p := m.picker
	switch key {
	case "esc", "q":
		m.picker = nil
	case "up", "k":
		p.cur = clamp(p.cur-1, len(p.items))
	case "down", "j":
		p.cur = clamp(p.cur+1, len(p.items))
	case "enter":
		m.form.get(p.target).setValue(p.items[p.cur])
		m.picker = nil
		m.form.move(1)
	}
	return nil
}

// ---------- utilità di formattazione ----------

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "oggi " + t.Format("15:04")
	case now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return "ieri " + t.Format("15:04")
	case now.AddDate(0, 0, 1).Format("20060102") == t.Format("20060102"):
		return "domani " + t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func colorizeLog(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch {
		case strings.Contains(l, "ERRORE") || strings.HasPrefix(l, "rsync:") || strings.HasPrefix(l, "rsync error"):
			lines[i] = sErr.Render(l)
		case strings.Contains(l, "ATTENZIONE") || strings.Contains(l, "attenzione"):
			lines[i] = sWarn.Render(l)
		case strings.HasPrefix(l, "*deleting"):
			lines[i] = sErr.Render("✖ cancella  ") + strings.TrimSpace(strings.TrimPrefix(l, "*deleting"))
		case len(l) > 12 && (l[0] == '>' || l[0] == 'c') && l[1] == 'f':
			tag := sOK.Render("+ copia     ")
			if strings.Contains(l[:12], "+++++++") {
				tag = sOK.Render("+ nuovo     ")
			} else {
				tag = sRun.Render("~ aggiorna  ")
			}
			lines[i] = tag + l[12:]
		case len(l) > 12 && l[0] == 'c' && l[1] == 'd':
			lines[i] = sMuted.Render("+ cartella  ") + l[12:]
		case len(l) > 12 && l[0] == '.' && l[11] == ' ':
			lines[i] = sMuted.Render("  invariato " + l[12:])
		case strings.HasPrefix(l, "[") || strings.HasPrefix(l, "#"):
			lines[i] = sMuted.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}
