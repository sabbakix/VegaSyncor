package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"vegasyncor/internal/config"
)

// Supporto del mouse: le parti cliccabili vengono marcate con zone.Mark durante il
// disegno; qui i clic vengono tradotti negli stessi tasti usati da tastiera, così il
// comportamento resta identico.

const doubleClickTime = 450 * time.Millisecond

// helpKeys sono i tasti mostrati nella barra dei comandi dell'ultima schermata
// disegnata (cliccabili). Viene riempito da renderHelp.
var helpKeys []string

// keyForLabel traduce l'etichetta di un tasto mostrata nella barra dei comandi
// nel tasto corrispondente; false se l'etichetta non è un'azione cliccabile.
func keyForLabel(label string) (tea.KeyMsg, bool) {
	switch label {
	case "invio":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}, true
	case "spazio":
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

// isDouble registra il clic su id e indica se è il secondo clic ravvicinato sulla stessa zona.
func (m *Model) isDouble(id string) bool {
	now := time.Now()
	double := m.lastClick == id && now.Sub(m.lastClickAt) < doubleClickTime
	m.lastClick, m.lastClickAt = id, now
	if double {
		m.lastClick = "" // un triplo clic non conta come secondo doppio clic
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
	// barra dei comandi (vale per ogni schermata, finestre comprese)
	for _, k := range helpKeys {
		if hit("key:"+k, msg) {
			if km, ok := keyForLabel(k); ok {
				return m.handleKey(km)
			}
		}
	}

	switch {
	case m.info != nil || m.confirm != nil || m.logOpen:
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

	for i := range tabNames {
		if hit(fmt.Sprintf("tab:%d", i), msg) {
			return m.handleKey(keyNamed(fmt.Sprint(i + 1)))
		}
	}
	if m.st == nil {
		return nil
	}
	// righe degli elenchi: clic = seleziona, doppio clic = apre
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

// formClick: clic su un campo lo attiva; caselle, scelte e giorni cambiano valore.
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
