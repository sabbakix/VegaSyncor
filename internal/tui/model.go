// Package tui implements the text-based management interface (usable over SSH).
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/api"
	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
)

type tab int

const (
	tabJobs tab = iota
	tabConns
	tabHistory
)

const tabCount = 3

func tabNames() []string { return []string{T("1 Syncs"), T("2 Connections"), T("3 History")} }

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
	stAt       time.Time // local time when st arrived
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

	lastClick string // to detect double clicks

	langPending bool // language change sent to the service, not yet confirmed
	lastClickAt time.Time
}

// ---------- messages ----------

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

// Run starts the TUI; mouse enables clicks and the wheel (terminals then
// require Shift + drag to select text).
func Run(c *api.Client, version string, mouse bool) error {
	zone.NewGlobal()
	defer zone.Close()
	zone.SetEnabled(mouse)
	setupTheme()
	m := &Model{client: c, version: version}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), tick())
}

// tick is aligned to the second, so the clock advances regularly.
func tick() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// zoneLabel describes the server time zone, e.g. "Europe/Rome, CEST".
func (m *Model) zoneLabel() string {
	if m.st == nil {
		return ""
	}
	switch {
	case m.st.ZoneName != "" && m.st.ZoneAbbr != "" && m.st.ZoneName != m.st.ZoneAbbr:
		return m.st.ZoneName + ", " + m.st.ZoneAbbr
	case m.st.ZoneName != "":
		return m.st.ZoneName
	}
	return m.st.ZoneAbbr
}

// clockNow returns the server time (see Model.serverNow); before the first
// status arrives it uses the local clock.
var clockNow = time.Now

// serverNow estimates the current server time, in its time zone, from the
// last status received plus the time elapsed since then.
func (m *Model) serverNow() time.Time {
	if m.st == nil || m.st.ServerTime.IsZero() {
		return time.Now()
	}
	return m.st.ServerTime.Add(time.Since(m.stAt))
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

// ---------- selection ----------

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
			m.st, m.stAt = msg.st, time.Now()
			clockNow = m.serverNow
			// the language is a service setting: follow it (unless forced with VEGASYNCOR_LANG)
			if i18n.FromEnv() == "" && m.st.Language != "" && m.st.Language != i18n.Lang() && !m.langPending {
				i18n.SetLang(m.st.Language)
			}
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
		m.setFlash(T("Saved."), false)
		return m, m.fetchStatus()

	case logMsg:
		if msg.err != nil {
			m.setFlash(T("Log:")+" "+msg.err.Error(), true)
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
			m.form.Err = T("no shares found: enter the name manually")
		} else {
			m.form.Err = ""
			m.picker = &picker{title: T("Available shares"), target: msg.target, items: msg.res.Shares}
		}
		return m, nil

	case testMsg:
		body := ""
		switch {
		case msg.err != nil:
			body = sErr.Render(T("ERROR:") + " " + msg.err.Error())
		case !msg.res.OK:
			body = sErr.Render(T("ERROR:") + " " + msg.res.Message)
			if !strings.Contains(msg.res.Message, "smbclient") {
				body += "\n\n" + sMuted.Render(T("Check host, user, password and domain."))
			}
		default:
			body = sOK.Render("OK: "+msg.res.Message) + "\n"
			for _, s := range msg.res.Shares {
				body += "\n  - " + s
			}
		}
		m.info = &infoBox{title: T("Connection test:") + " " + msg.name, body: body}
		return m, nil

	case langMsg:
		m.langPending = false
		if msg.err != nil {
			m.setFlash(T("Language not saved:")+" "+msg.err.Error(), true)
		}
		return m, m.fetchStatus()

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

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
	case "L":
		return m.cycleLanguage()
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		if m.tab == tabHistory {
			return m.fetchHistory()
		}
		return nil
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
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
		m.form, m.formKind, m.formID = newJobForm(nil, m.conns(), m.zoneLabel()), "job", ""
	case "enter", "e":
		if j != nil {
			jj := j.Job
			m.form, m.formKind, m.formID = newJobForm(&jj, m.conns(), m.zoneLabel()), "job", j.Job.ID
		}
	case "r":
		if j != nil {
			id := j.Job.ID
			return m.do(T("Started:")+" "+j.Job.Name, func() error { return m.client.RunJob(id, false) })
		}
	case "s":
		if j != nil {
			id := j.Job.ID
			return m.do(Tf("Dry run started: %s (press l to see the result)", j.Job.Name), func() error { return m.client.RunJob(id, true) })
		}
	case "x":
		if j != nil && j.Current != nil {
			id, name := j.Job.ID, j.Job.Name
			m.confirm = &confirmBox{
				text:   Tf("Stop the run of %q?", name),
				action: m.do(T("Cancellation requested"), func() error { return m.client.CancelJob(id) }),
			}
		}
	case "p", " ":
		if j != nil {
			id, en := j.Job.ID, !j.Job.Enabled
			msg := T("Job resumed")
			if !en {
				msg = T("Job paused (it will not run on schedule)")
			}
			return m.do(msg, func() error { return m.client.SetJobEnabled(id, en) })
		}
	case "d", "delete":
		if j != nil {
			id, name := j.Job.ID, j.Job.Name
			m.confirm = &confirmBox{
				text:   Tf("Delete the sync %q?\nFiles already copied to the destination are NOT touched.", name),
				action: m.do(T("Deleted:")+" "+name, func() error { return m.client.DeleteJob(id) }),
			}
		}
	case "l":
		if j != nil {
			if j.Current != nil {
				return m.openLog(j.Current.ID, j.Job.Name+" – "+T("running"))
			}
			if j.Last != nil {
				return m.openLog(j.Last.ID, j.Job.Name+" – "+fmtTime(j.Last.Start))
			}
			m.setFlash(T("No runs for this job"), true)
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
			m.setFlash(Tf("Testing %s…", name), false)
			return func() tea.Msg {
				res, err := m.client.TestConnection(id)
				return testMsg{name: name, res: res, err: err}
			}
		}
	case "d", "delete":
		if c != nil {
			id, name := c.ID, c.Name
			m.confirm = &confirmBox{
				text:   Tf("Delete the connection %q and its saved password?", name),
				action: m.do(T("Connection deleted"), func() error { return m.client.DeleteConnection(id) }),
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
			// a writable source is allowed but must be confirmed
			m.confirm = &confirmBox{
				text:   T("The source will NOT be protected as read-only.\nSave anyway?"),
				action: func() tea.Msg { _, err := c.SaveJob(j); return savedMsg{err} },
			}
			return nil
		}
		m.saving = true
		return func() tea.Msg { _, err := c.SaveJob(j); return savedMsg{err} }
	case "conn":
		in := connFromForm(fm, m.formID)
		if m.formID == "" && in.Password == "" {
			fm.Err = T("enter the password")
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
			fm.Err = T("create a connection first")
			return nil
		}
		fm.Err = T("reading the shares…")
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
			fm.Err = T("select the connection and the share first")
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

// ---------- formatting helpers ----------

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	now := clockNow()
	t = t.In(now.Location())
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return T("today") + " " + t.Format("15:04")
	case now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return T("yesterday") + " " + t.Format("15:04")
	case now.AddDate(0, 0, 1).Format("20060102") == t.Format("20060102"):
		return T("tomorrow") + " " + t.Format("15:04")
	}
	return t.Format(i18n.ShortDateTimeLayout())
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
		case isLogError(l):
			lines[i] = sErr.Render(l)
		case isLogWarning(l):
			lines[i] = sWarn.Render(l)
		case strings.HasPrefix(l, "*deleting"):
			lines[i] = sErr.Render("- "+pad(T("delete"), 10)) + strings.TrimSpace(strings.TrimPrefix(l, "*deleting"))
		case len(l) > 12 && (l[0] == '>' || l[0] == 'c') && l[1] == 'f':
			tag := sRun.Render("~ " + pad(T("update"), 10))
			if strings.Contains(l[:12], "+++++++") {
				tag = sOK.Render("+ " + pad(T("new"), 10))
			}
			lines[i] = tag + l[12:]
		case len(l) > 12 && l[0] == 'c' && l[1] == 'd':
			lines[i] = sMuted.Render("+ "+pad(T("folder"), 10)) + l[12:]
		case len(l) > 12 && l[0] == '.' && l[11] == ' ':
			lines[i] = sMuted.Render("  " + pad(T("unchanged"), 10) + l[12:])
		case isRsyncStat(l):
			lines[i] = sMuted.Render(l)
		case strings.HasPrefix(l, "[") || strings.HasPrefix(l, "#"):
			lines[i] = sMuted.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

var rsyncStatPrefixes = []string{"Number of ", "Total ", "Literal data", "Matched data", "File list ", "sent ", "total size is"}

// isRsyncStat recognises the final rsync statistics lines (shown dimmed).
func isRsyncStat(l string) bool {
	for _, p := range rsyncStatPrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// isLogError / isLogWarning recognise error and warning lines in a run log,
// in any language (old logs may have been written in another language).
func isLogError(l string) bool {
	return strings.Contains(l, "ERROR") || strings.Contains(l, "ERRORE") ||
		strings.HasPrefix(l, "rsync:") || strings.HasPrefix(l, "rsync error")
}

func isLogWarning(l string) bool {
	low := strings.ToLower(l)
	return strings.Contains(low, "warning") || strings.Contains(low, "attenzione")
}

type langMsg struct {
	code string
	err  error
}

// cycleLanguage switches to the next language and saves it as a service setting.
func (m *Model) cycleLanguage() tea.Cmd {
	cur := 0
	for i, l := range i18n.Languages {
		if l.Code == i18n.Lang() {
			cur = i
		}
	}
	return m.setLanguage(i18n.Languages[(cur+1)%len(i18n.Languages)].Code)
}

func (m *Model) setLanguage(code string) tea.Cmd {
	if code == i18n.Lang() {
		return nil
	}
	i18n.SetLang(code)
	m.langPending = true
	c := m.client
	return func() tea.Msg { return langMsg{code, c.SetLanguage(code)} }
}
