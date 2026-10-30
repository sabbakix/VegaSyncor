package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"vegasyncor/internal/api"
)

// OneDrive sign-in, from the connection form (field "od_account"). The service
// returns the Microsoft sign-in address: the user opens it in a browser on any
// device, signs in, and pastes back the address the browser lands on.

type odSignIn struct {
	session string
	url     string
	input   textinput.Model
	busy    bool
	failed  bool // the sign-in must be started again
	err     string
	copied  bool
}

type odStartMsg struct {
	start *api.OneDriveStart
	err   error
}

type odFinishMsg struct {
	acc *api.OneDriveAccount
	err error
}

func (m *Model) startSignIn() tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "http://localhost:53682/?code=..."
	in.PlaceholderStyle = sPlaceholderOn
	in.CharLimit = 4096
	m.signin = &odSignIn{busy: true, input: in}
	c := m.client
	return func() tea.Msg {
		s, err := c.OneDriveSignIn()
		return odStartMsg{s, err}
	}
}

func (m *Model) signInStarted(msg odStartMsg) tea.Cmd {
	s := m.signin
	if s == nil {
		return nil
	}
	s.busy = false
	if msg.err != nil {
		s.err, s.failed = msg.err.Error(), true
		return nil
	}
	s.session, s.url = msg.start.Session, msg.start.URL
	return s.input.Focus()
}

func (m *Model) signInFinished(msg odFinishMsg) {
	s := m.signin
	if s == nil {
		return
	}
	s.busy = false
	if msg.err != nil {
		s.err, s.failed = msg.err.Error(), true
		return
	}
	m.signin = nil
	m.odAccount = msg.acc
	if m.form != nil {
		if f := m.form.get("od_account"); f != nil {
			f.Text = accountLabel(msg.acc.Account, msg.acc.DriveType)
		}
		if f := m.form.get("name"); f != nil && f.value() == "" {
			f.setValue("OneDrive " + msg.acc.Account)
		}
	}
	m.setFlash(Tf("Signed in to OneDrive as %s: save the connection with ctrl+s", msg.acc.Account), false)
}

func accountLabel(account, driveType string) string {
	return account + "  (OneDrive " + driveTypeLabel(driveType) + ")"
}

// copyLink puts the sign-in address in the clipboard of the user's computer
// (OSC 52: works over SSH in most terminals).
func (m *Model) copyLink() {
	if s := m.signin; s != nil && s.url != "" {
		termenv.Copy(s.url)
		s.copied = true
	}
}

func (m *Model) signInKey(k tea.KeyMsg) tea.Cmd {
	s := m.signin
	switch k.String() {
	case "esc":
		m.signin = nil
		return nil
	case "ctrl+k":
		m.copyLink()
		return nil
	case "enter":
		if s.busy {
			return nil
		}
		if s.failed {
			return m.startSignIn() // a new link
		}
		pasted := strings.TrimSpace(s.input.Value())
		if pasted == "" {
			s.err = T("paste the address shown by the browser after the sign-in")
			return nil
		}
		s.busy, s.err = true, ""
		c, session := m.client, s.session
		return func() tea.Msg {
			acc, err := c.OneDriveFinish(session, pasted)
			return odFinishMsg{acc, err}
		}
	}
	if s.busy || s.failed {
		return nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(k)
	return cmd
}

// viewSignIn is a full screen without borders, so that the long address can be
// selected and copied from the terminal without extra characters.
func (m *Model) viewSignIn() (string, string) {
	s := m.signin
	help := T("ctrl+k copy link · enter confirm · esc cancel")
	var b strings.Builder
	b.WriteString(" " + sTitle.Render(T("Sign in to OneDrive")) + "\n\n")
	switch {
	case s.busy && s.url == "":
		b.WriteString(" " + sMuted.Render(T("preparing the sign-in…")) + "\n")
		return b.String(), help
	case s.url == "":
		b.WriteString(" " + sErr.Render(s.err) + "\n\n " + T("Press enter to try again.") + "\n")
		return b.String(), T("enter try again · esc cancel")
	}
	b.WriteString(" " + sBold.Render("1.") + " " + T("Open this address in a browser, on your PC or phone:") + "\n")
	b.WriteString(" " + sMuted.Render(T("(select it with the mouse — hold Shift while selecting — or press ctrl+k to copy it)")) + "\n\n")
	for _, l := range wrapURL(s.url, m.w) {
		b.WriteString(sKey.Render(l) + "\n") // no indent: the copied text must be only the address
	}
	b.WriteString("\n")
	copyBtn := zone.Mark("odcopy", sKey.Render(T("[copy link]")))
	if s.copied {
		copyBtn += "  " + sOK.Render(T("copied (if your terminal allows it)"))
	}
	b.WriteString(" " + copyBtn + "\n\n")
	b.WriteString(" " + sBold.Render("2.") + " " + T("Sign in with the Microsoft account (personal or work) and accept.") + "\n")
	b.WriteString(" " + sBold.Render("3.") + " " + T("The browser then shows an error page (\"localhost\" cannot be reached): that is expected.") + "\n")
	b.WriteString("    " + T("Copy the whole address from the address bar and paste it here:") + "\n\n")
	in := s.input
	in.Width = max(m.w-6, 20)
	b.WriteString("  " + sKey.Render(">") + " " + in.View() + "\n\n")
	switch {
	case s.busy:
		b.WriteString(" " + sMuted.Render(T("signing in…")) + "\n")
	case s.failed:
		b.WriteString(" " + sErr.Render(s.err) + "\n " + T("Press enter to get a new link and try again.") + "\n")
		help = T("enter new link · esc cancel")
	case s.err != "":
		b.WriteString(" " + sErr.Render(s.err) + "\n")
	}
	return b.String(), help
}

// wrapURL splits the address in lines of the screen width.
func wrapURL(u string, w int) []string {
	w = max(w, 20)
	var out []string
	for runewidth.StringWidth(u) > w {
		out = append(out, u[:w])
		u = u[w:]
	}
	return append(out, u)
}
