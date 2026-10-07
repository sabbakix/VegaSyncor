package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
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
	DayCur           int // index in config.WeekOrder
	Browse           bool
	Visible          func(f *form) bool
	orig             string // value when the form was opened (to show changed fields)
	LabelFn          func(f *form) string
	HelpFn           func(f *form) string // description depending on the chosen value
}

type form struct {
	Title  string
	Fields []*field
	Cur    int
	Err    string
	warned bool // a warning was shown: the next save goes ahead
	offset int
}

func newText(key, label, value, placeholder string) *field {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Placeholder = placeholder
	ti.PlaceholderStyle = sPlaceholder
	ti.Prompt = ""
	ti.CharLimit = 512
	return &field{Key: key, Label: label, Kind: fText, Input: ti}
}

func newPassword(key, label, placeholder string) *field {
	f := newText(key, label, "", placeholder)
	f.Kind = fPassword
	f.Input.EchoMode = textinput.EchoPassword
	f.Input.EchoCharacter = '*'
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
func (f *field) helpFn(fn func(*form) string) *field  { f.HelpFn = fn; return f }
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
	for _, f := range fm.Fields {
		f.orig = f.snapshot()
	}
	fm.Cur = -1
	fm.move(1)
}

// snapshot returns the current value of a field as a comparable string.
func (f *field) snapshot() string {
	switch f.Kind {
	case fText, fPassword:
		return f.Input.Value()
	case fChoice:
		return f.choice()
	case fBool:
		return fmt.Sprint(f.Bool)
	case fDays:
		return fmt.Sprint(f.Days)
	}
	return ""
}

// modified reports whether the field differs from its value when the form was opened.
func (f *field) modified() bool { return f.Kind != fSection && f.snapshot() != f.orig }

// sectionOf returns the number of section headers before field i
// (0 = before the first section).
func (fm *form) sectionOf(i int) int {
	n := 0
	for k := 0; k <= i && k < len(fm.Fields); k++ {
		if fm.Fields[k].Kind == fSection {
			n++
		}
	}
	return n
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
		prev := fm.Fields[fm.Cur]
		prev.Input.Blur()
		prev.Input.PlaceholderStyle = sPlaceholder
	}
	fm.Cur = i
	f := fm.Fields[i]
	if f.Kind == fText || f.Kind == fPassword {
		f.Input.Focus()
		f.Input.PlaceholderStyle = sPlaceholderOn // readable on the highlighted row
	}
}

// update handles navigation and editing of the fields.
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
		case "a": // all / none
			all := true
			for _, v := range f.Days {
				all = all && v
			}
			for i := range f.Days {
				f.Days[i] = !all
			}
		case "w": // Mon-Fri
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

// render returns the form lines; height limits the height (scroll).
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
		// marker column: ">" = active field, "*" = changed since the form was opened
		changed := f.modified()
		marker := " "
		if focused {
			marker = sKey.Render(">")
		}
		if changed {
			marker += sWarn.Render("*")
		} else {
			marker += " "
		}
		labelStyle := sMuted
		if changed {
			labelStyle = sWarn
		}
		if focused {
			labelStyle = labelStyle.Bold(true)
			if !changed {
				labelStyle = sBold
			}
		}
		label := labelStyle.Render(pad(text, labelWidth))
		valW := width - labelWidth - 3
		line := marker + label + " " + fm.renderValue(i, f, focused, valW)
		if focused {
			line = highlightRow(line, width)
		}
		lines = append(lines, zone.Mark(fmt.Sprintf("field:%d", i), line))
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

func (fm *form) renderValue(i int, f *field, focused bool, w int) string {
	switch f.Kind {
	case fText, fPassword:
		hint := T("[browse]")
		if !f.Browse {
			f.Input.Width = max(w-2, 10)
			return f.Input.View()
		}
		// the text input takes the whole width: shrink it to make room for
		// the button, otherwise it would end up off screen
		f.Input.Width = max(w-2-runewidth.StringWidth(hint)-2, 10)
		btn := sMuted
		if focused {
			btn = sKey
		}
		return f.Input.View() + "  " + zone.Mark(fmt.Sprintf("browse:%d", i), btn.Render(hint))
	case fChoice:
		lbl := f.Options[f.Sel].Label
		if focused {
			return zone.Mark(fmt.Sprintf("fprev:%d", i), sKey.Render("< ")) + sSel.Render(" "+lbl+" ") +
				zone.Mark(fmt.Sprintf("fnext:%d", i), sKey.Render(" >"))
		}
		return lbl
	case fBool:
		return checkbox(f.Bool)
	case fDays:
		var parts []string
		for idx, d := range config.WeekOrder {
			name := config.DayName(d)
			if !f.Days[d] {
				name = sMuted.Render(name)
			}
			s := checkbox(f.Days[d]) + " " + name
			if focused && idx == f.DayCur {
				s = sSel.Render(s)
			}
			parts = append(parts, zone.Mark(fmt.Sprintf("day:%d:%d", i, idx), s))
		}
		return strings.Join(parts, "  ")
	}
	return ""
}

// checkbox draws a check box with ASCII characters only ("[x]" / "[ ]"),
// readable in any terminal and font, also over SSH.
func checkbox(on bool) string {
	if on {
		return "[" + sCheck.Render("x") + "]"
	}
	return "[ ]"
}

// description returns the help text of the active field, shown in the fixed
// bar at the bottom of the form (so the fields do not move).
func (fm *form) description() (label, text string) {
	if fm.Cur < 0 || fm.Cur >= len(fm.Fields) {
		return "", ""
	}
	f := fm.Fields[fm.Cur]
	label = f.Label
	if f.LabelFn != nil {
		label = f.LabelFn(fm)
	}
	var hints []string
	if f.HelpFn != nil {
		hints = append(hints, f.HelpFn(fm))
	}
	if f.Help != "" {
		hints = append(hints, f.Help)
	}
	switch f.Kind {
	case fChoice:
		hints = append(hints, T("← → to change the choice"))
	case fBool:
		hints = append(hints, T("space to toggle"))
	case fDays:
		hints = append(hints, T("← → to move, space to select · w = Mon-Fri · a = all/none"))
	}
	return label, strings.Join(hints, " · ")
}
