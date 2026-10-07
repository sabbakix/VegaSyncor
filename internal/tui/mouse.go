package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/config"
	"vegasyncor/internal/i18n"
)

// Mouse support: the clickable parts are marked with zone.Mark while drawing;
// here clicks are translated into the same keys used from the keyboard, so the
// behaviour stays identical.

const doubleClickTime = 450 * time.Millisecond

// helpKeys are the keys shown in the command bar of the last drawn screen
// (clickable). It is filled by renderHelp.
var helpKeys []string

// keyForLabel translates the label of a key shown in the command bar into the
// corresponding key (labels in any language); false if it is not a clickable action.
func keyForLabel(label string) (tea.KeyMsg, bool) {
	switch label {
	case "enter", "invio":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}, true
	case "space", "spazio":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	case "←":
		return tea.KeyMsg{Type: tea.KeyLeft}, true
	}
	if r := []rune(label); len(r) == 1 && r[0] < 128 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	return tea.KeyMsg{}, false
}

func keyNamed(name string) tea.KeyMsg {
	switch name {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func hit(id string, msg tea.MouseMsg) bool {
	z := zone.Get(id)
	return z != nil && z.InBounds(msg)
}

// isDouble records a click on id and reports whether it is a quick second click on the same zone.
func (m *Model) isDouble(id string) bool {
	now := time.Now()
	double := m.lastClick == id && now.Sub(m.lastClickAt) < doubleClickTime
	m.lastClick, m.lastClickAt = id, now
	if double {
		m.lastClick = "" // a triple click does not count as a second double click
	}
	return double
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if m.logOpen {
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			return cmd
		}
		dir := "down"
		if msg.Button == tea.MouseButtonWheelUp {
			dir = "up"
		}
		return m.handleKey(keyNamed(dir))
	case tea.MouseButtonLeft:
		return m.handleClick(msg)
	}
	return nil
}

func (m *Model) handleClick(msg tea.MouseMsg) tea.Cmd {
	// command bar (valid on every screen, dialogs included)
	for _, k := range helpKeys {
		if hit("key:"+k, msg) {
			if km, ok := keyForLabel(k); ok {
				return m.handleKey(km)
			}
		}
	}

	if m.logOpen {
		for f := lfAll; f < logFilterCount; f++ {
			if hit("logf:"+f.key(), msg) {
				m.setLogFilter(f)
			}
		}
		return nil
	}
	switch {
	case m.info != nil || m.confirm != nil:
		return nil
	}
	if _, pending := m.fwPendingLeft(); pending && m.form == nil {
		return nil // the firewall confirmation dialog is open
	}
	switch {
	case false:
		return nil
	case m.picker != nil:
		for i := range m.picker.items {
			id := fmt.Sprintf("pick:%d", i)
			if hit(id, msg) {
				m.picker.cur = i
				if m.isDouble(id) {
					return m.pickerKey("enter")
				}
				return nil
			}
		}
		return nil
	case m.browser != nil:
		for i := range m.browser.items() {
			id := fmt.Sprintf("br:%d", i)
			if hit(id, msg) {
				m.browser.cur = i
				if m.isDouble(id) {
					return m.browserKey("enter")
				}
				return nil
			}
		}
		return nil
	case m.form != nil:
		return m.formClick(msg)
	}

	for _, l := range i18n.Languages {
		if hit("lang:"+l.Code, msg) {
			return m.setLanguage(l.Code)
		}
	}
	for i := range tabNames() {
		if hit(fmt.Sprintf("tab:%d", i), msg) {
			return m.handleKey(keyNamed(fmt.Sprint(i + 1)))
		}
	}
	if m.st == nil {
		return nil
	}
	// list rows: click = select, double click = open
	switch m.tab {
	case tabJobs:
		for i := range m.jobs() {
			if id := fmt.Sprintf("job:%d", i); hit(id, msg) {
				m.jobCur = i
				if m.isDouble(id) {
					return m.jobsKey("enter")
				}
				return nil
			}
		}
	case tabConns:
		for i := range m.conns() {
			if id := fmt.Sprintf("conn:%d", i); hit(id, msg) {
				m.connCur = i
				if m.isDouble(id) {
					return m.connsKey("enter")
				}
				return nil
			}
		}
	case tabFirewall:
		for i := range m.fwItems() {
			if id := fmt.Sprintf("fw:%d", i); hit(id, msg) {
				m.fwCur = i
				if m.isDouble(id) {
					return m.firewallKey("enter")
				}
				return nil
			}
		}
	case tabHistory:
		for i := range m.history {
			if id := fmt.Sprintf("hist:%d", i); hit(id, msg) {
				m.histCur = i
				if m.isDouble(id) {
					return m.historyKey("enter")
				}
				return nil
			}
		}
	}
	return nil
}

// formClick: a click on a field activates it; boxes, choices and days change value.
func (m *Model) formClick(msg tea.MouseMsg) tea.Cmd {
	fm := m.form
	for i, f := range fm.Fields {
		if !f.focusable() || !fm.visible(f) {
			continue
		}
		switch {
		case hit(fmt.Sprintf("browse:%d", i), msg):
			fm.setFocus(i)
			fm.Err = ""
			return m.startBrowse(f.Key)
		case hit(fmt.Sprintf("fprev:%d", i), msg):
			fm.setFocus(i)
			f.Sel = (f.Sel - 1 + len(f.Options)) % len(f.Options)
			return nil
		case hit(fmt.Sprintf("fnext:%d", i), msg):
			fm.setFocus(i)
			f.Sel = (f.Sel + 1) % len(f.Options)
			return nil
		}
		if f.Kind == fDays {
			for idx := range 7 {
				if hit(fmt.Sprintf("day:%d:%d", i, idx), msg) {
					fm.setFocus(i)
					f.DayCur = idx
					d := config.WeekOrder[idx]
					f.Days[d] = !f.Days[d]
					return nil
				}
			}
		}
		if hit(fmt.Sprintf("field:%d", i), msg) {
			wasFocused := fm.Cur == i
			fm.setFocus(i)
			fm.Err = ""
			switch f.Kind {
			case fBool:
				f.Bool = !f.Bool
			case fChoice:
				if wasFocused {
					f.Sel = (f.Sel + 1) % len(f.Options)
				}
			}
			return nil
		}
	}
	return nil
}
