package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"vegasyncor/internal/config"
)

type fieldKind int

const (
	fSection fieldKind = iota
	fText
	fPassword
	fChoice
	fBool
	fDays
)

type option struct {
	Value, Label string
}

type field struct {
	Key, Label, Help string
	Kind             fieldKind
	Input            textinput.Model
	Options          []option
	Sel              int
	Bool             bool
	Days             [7]bool
	DayCur           int // indice in config.WeekOrder
	Browse           bool
	Visible          func(f *form) bool
	LabelFn          func(f *form) string
}

type form struct {
	Title  string
	Fields []*field
	Cur    int
	Err    string
	offset int
}

func newText(key, label, value, placeholder string) *field {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Placeholder = placeholder
	ti.Prompt = ""
	ti.CharLimit = 512
	return &field{Key: key, Label: label, Kind: fText, Input: ti}
}

func newPassword(key, label, placeholder string) *field {
	f := newText(key, label, "", placeholder)
	f.Kind = fPassword
	f.Input.EchoMode = textinput.EchoPassword
	f.Input.EchoCharacter = '•'
	return f
}

func newChoice(key, label string, opts []option, value string) *field {
	f := &field{Key: key, Label: label, Kind: fChoice, Options: opts}
	for i, o := range opts {
		if o.Value == value {
			f.Sel = i
		}
	}
	return f
}

func newBool(key, label string, v bool) *field {
	return &field{Key: key, Label: label, Kind: fBool, Bool: v}
}

func newDays(key, label string, days []int) *field {
	f := &field{Key: key, Label: label, Kind: fDays}
	for _, d := range days {
		if d >= 0 && d < 7 {
			f.Days[d] = true
		}
	}
	return f
}

func section(label string) *field { return &field{Kind: fSection, Label: label} }

func (f *field) withHelp(h string) *field             { f.Help = h; return f }
func (f *field) when(fn func(*form) bool) *field      { f.Visible = fn; return f }
func (f *field) labeled(fn func(*form) string) *field { f.LabelFn = fn; return f }
func (f *field) browsable() *field                    { f.Browse = true; return f }
func (f *field) focusable() bool                      { return f.Kind != fSection }
func (f *field) value() string                        { return strings.TrimSpace(f.Input.Value()) }
func (f *field) choice() string                       { return f.Options[f.Sel].Value }
func (f *field) setValue(v string)                    { f.Input.SetValue(v); f.Input.CursorEnd() }
func (fm *form) visible(f *field) bool                { return f.Visible == nil || f.Visible(fm) }
func (fm *form) get(key string) *field {
	for _, f := range fm.Fields {
		if f.Key == key {
			return f
		}
	}
	return nil
}
func (fm *form) val(key string) string    { return fm.get(key).value() }
func (fm *form) choice(key string) string { return fm.get(key).choice() }
func (fm *form) current() *field          { return fm.Fields[fm.Cur] }

func (f *field) days() []int {
	var out []int
	for d, on := range f.Days {
		if on {
			out = append(out, d)
		}
	}
	return out
}

func (fm *form) init() {
	fm.Cur = -1
	fm.move(1)
}

func (fm *form) move(dir int) {
	n := len(fm.Fields)
	i := fm.Cur
	for range n {
		i = (i + dir + n) % n
		if f := fm.Fields[i]; f.focusable() && fm.visible(f) {
			fm.setFocus(i)
			return
		}
	}
}

func (fm *form) setFocus(i int) {
	if fm.Cur >= 0 && fm.Cur < len(fm.Fields) {
		fm.Fields[fm.Cur].Input.Blur()
	}
	fm.Cur = i
	f := fm.Fields[i]
	if f.Kind == fText || f.Kind == fPassword {
		f.Input.Focus()
	}
}

// update gestisce la navigazione e la modifica dei campi.
func (fm *form) update(msg tea.KeyMsg) tea.Cmd {
	f := fm.current()
	switch msg.String() {
	case "tab", "down":
		fm.move(1)
		return nil
	case "shift+tab", "up":
		fm.move(-1)
		return nil
	case "enter":
		if !f.Browse {
			fm.move(1)
		}
		return nil
	}
	switch f.Kind {
	case fChoice:
		switch msg.String() {
		case "right", "l", " ":
			f.Sel = (f.Sel + 1) % len(f.Options)
		case "left", "h":
			f.Sel = (f.Sel - 1 + len(f.Options)) % len(f.Options)
		}
	case fBool:
		switch msg.String() {
		case " ", "right", "left", "x":
			f.Bool = !f.Bool
		}
	case fDays:
		switch msg.String() {
		case "right", "l":
			f.DayCur = (f.DayCur + 1) % 7
		case "left", "h":
			f.DayCur = (f.DayCur + 6) % 7
		case " ", "x":
			d := config.WeekOrder[f.DayCur]
			f.Days[d] = !f.Days[d]
		case "a": // tutti / nessuno
			all := true
			for _, v := range f.Days {
				all = all && v
			}
			for i := range f.Days {
				f.Days[i] = !all
			}
		case "w": // lun-ven
			f.Days = [7]bool{false, true, true, true, true, true, false}
		}
	case fText, fPassword:
		var cmd tea.Cmd
		f.Input, cmd = f.Input.Update(msg)
		return cmd
	}
	return nil
}

const labelWidth = 24

// render restituisce le righe del form; height limita l'altezza (scroll).
func (fm *form) render(width, height int) string {
	var lines []string
	focusLine := 0
	for i, f := range fm.Fields {
		if !fm.visible(f) {
			continue
		}
		if f.Kind == fSection {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, sSection.Render(f.Label)+" "+rule(width-runewidth.StringWidth(f.Label)-1))
			continue
		}
		focused := i == fm.Cur
		if focused {
			focusLine = len(lines)
		}
		text := f.Label
		if f.LabelFn != nil {
			text = f.LabelFn(fm)
		}
		marker := "  "
		label := sMuted.Render(pad(text, labelWidth))
		if focused {
			marker = sKey.Render("▸ ")
			label = sBold.Render(pad(text, labelWidth))
		}
		valW := width - labelWidth - 3
		lines = append(lines, marker+label+" "+fm.renderValue(f, focused, valW))
		if focused && f.Help != "" {
			lines = append(lines, strings.Repeat(" ", labelWidth+3)+sMuted.Render(trunc(f.Help, valW)))
		}
	}
	if height > 0 && len(lines) > height {
		if focusLine < fm.offset {
			fm.offset = focusLine
		}
		if focusLine+2 >= fm.offset+height {
			fm.offset = focusLine + 3 - height
		}
		fm.offset = max(0, min(fm.offset, len(lines)-height))
		lines = lines[fm.offset : fm.offset+height]
	} else {
		fm.offset = 0
	}
	return strings.Join(lines, "\n")
}

func (fm *form) renderValue(f *field, focused bool, w int) string {
	switch f.Kind {
	case fText, fPassword:
		f.Input.Width = max(w-2, 10)
		v := f.Input.View()
		if f.Browse && focused {
			v += "  " + sMuted.Render("[Invio: sfoglia]")
		}
		return v
	case fChoice:
		lbl := f.Options[f.Sel].Label
		if focused {
			return sKey.Render("◀ ") + sSel.Render(" "+lbl+" ") + sKey.Render(" ▶")
		}
		return lbl
	case fBool:
		box := "[ ]"
		if f.Bool {
			box = "[" + sOK.Render("✔") + "]"
		}
		if focused {
			return box + sMuted.Render("  spazio per cambiare")
		}
		return box
	case fDays:
		var parts []string
		for idx, d := range config.WeekOrder {
			name := config.DayNames[d]
			s := name
			if f.Days[d] {
				s = sOK.Render("●" + name)
			} else {
				s = sMuted.Render("○" + name)
			}
			if focused && idx == f.DayCur {
				s = sSel.Render(s)
			}
			parts = append(parts, s)
		}
		out := strings.Join(parts, " ")
		if focused {
			out += sMuted.Render("  ←→ spazio · w=lun-ven · a=tutti")
		}
		return out
	}
	return ""
}
