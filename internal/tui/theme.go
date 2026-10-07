package tui

import (
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The default theme paints its own dark blue-grey background on the whole screen,
// so the TUI looks the same whatever the terminal background is.
// VEGASYNCOR_THEME=terminal keeps the terminal's own background and colours.

var (
	// dark blue-grey; with 256 colours the automatic conversion gives navy (17),
	// a dark grey is closer to the look of the theme
	themeBg   = lipgloss.CompleteColor{TrueColor: "#1B2333", ANSI256: "235", ANSI: "0"}
	themeText = lipgloss.Color("#DCE3EE")
)

// themeOn reports whether the painted background is in use.
var themeOn bool

// themeBase is the escape sequence selecting the theme background and text colour
// for the current terminal colour profile ("" if the terminal has no colours).
var themeBase string

// setupColors chooses the colour mode. SSH passes TERM (usually xterm-256color)
// but not COLORTERM, so terminals that support true colour (nearly all current
// ones) end up with the 256-colour approximation: in that case true colour is
// assumed. VEGASYNCOR_COLORS=truecolor|256|16|none forces a mode.
func setupColors() {
	switch strings.ToLower(os.Getenv("VEGASYNCOR_COLORS")) {
	case "truecolor", "24bit":
		lipgloss.SetColorProfile(termenv.TrueColor)
	case "256":
		lipgloss.SetColorProfile(termenv.ANSI256)
	case "16":
		lipgloss.SetColorProfile(termenv.ANSI)
	case "none", "0":
		lipgloss.SetColorProfile(termenv.Ascii)
	case "":
		term := os.Getenv("TERM")
		multiplexer := strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") // may drop 24-bit colours
		if lipgloss.ColorProfile() == termenv.ANSI256 && os.Getenv("COLORTERM") == "" &&
			strings.Contains(term, "256color") && !multiplexer {
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	}
}

// setupTheme chooses the theme at startup.
func setupTheme() {
	setupColors()
	if strings.EqualFold(os.Getenv("VEGASYNCOR_THEME"), "terminal") {
		return
	}
	// render a sample with the theme colours and keep only the opening sequence:
	// it follows the colour profile detected by lipgloss (true colour, 256, 16)
	sample := lipgloss.NewStyle().Background(themeBg).Foreground(themeText).Render("x")
	i := strings.Index(sample, "x")
	if i <= 0 {
		return // no colour support: nothing to paint
	}
	themeBase = sample[:i]
	themeOn = true
	// our background is dark: use the dark variant of the adaptive colours
	lipgloss.SetHasDarkBackground(true)
	cFaint = lipgloss.AdaptiveColor{Light: "#3D4A63", Dark: "#3D4A63"}
	cMuted = lipgloss.AdaptiveColor{Light: "#8E9AB0", Dark: "#8E9AB0"}
	cSelBg = lipgloss.AdaptiveColor{Light: "#2D4A78", Dark: "#2D4A78"}
	cText = lipgloss.AdaptiveColor{Light: "#DCE3EE", Dark: "#DCE3EE"}
	cAccent = lipgloss.AdaptiveColor{Light: "#6CB6FF", Dark: "#6CB6FF"}
	// Explicit 256-colour values: the automatic conversion turns these blue-greys
	// into teal (#005f5f), which also hides the placeholders. Colour 60 (#5f5f87)
	// is the blue-grey of the 256-colour palette.
	cRowBg = lipgloss.CompleteColor{TrueColor: "#2B3650", ANSI256: "60", ANSI: "4"}
	cPlaceholder = lipgloss.CompleteColor{TrueColor: "#8E9AB0", ANSI256: "245", ANSI: "8"}
	cPlaceholderOn = lipgloss.CompleteColor{TrueColor: "#B4BECE", ANSI256: "250", ANSI: "7"}
	rebuildStyles()
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// paintBackground applies the theme background to a rendered screen: after every
// sequence that resets the colours it selects the theme colours again, and it
// fills every line (and the missing lines) up to the screen size.
func paintBackground(screen string, w, h int) string {
	if !themeOn || w <= 0 {
		return screen
	}
	lines := strings.Split(screen, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = paintLine(l, w, themeBase)
	}
	return strings.Join(lines, "\n")
}

// paintLine gives a line the colours selected by base (also after every colour
// reset inside it) and fills it with spaces up to width w.
func paintLine(l string, w int, base string) string {
	l = sgrRe.ReplaceAllStringFunc(l, func(seq string) string {
		if resetsColours(sgrRe.FindStringSubmatch(seq)[1]) {
			return seq + base
		}
		return seq
	})
	if fill := w - lipgloss.Width(l); fill > 0 {
		l += strings.Repeat(" ", fill)
	}
	return base + l + "\x1b[0m"
}

// highlightRow gives the active row of a form a subtle background across the whole width.
func highlightRow(line string, w int) string {
	sample := lipgloss.NewStyle().Background(cRowBg).Render("x")
	i := strings.Index(sample, "x")
	if i <= 0 {
		return line // no colour support
	}
	return paintLine(line, w, sample[:i])
}

// resetsColours reports whether an SGR parameter list resets the background or the
// foreground ("", 0, 39, 49), skipping the arguments of 38/48 extended colours.
func resetsColours(params string) bool {
	if params == "" {
		return true
	}
	p := strings.Split(params, ";")
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case "38", "48":
			if i+1 < len(p) && p[i+1] == "2" {
				i += 4
			} else {
				i += 2
			}
		case "0", "", "39", "49":
			return true
		}
	}
	return false
}
