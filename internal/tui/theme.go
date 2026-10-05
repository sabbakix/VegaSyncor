package tui

import (
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The default theme paints its own dark blue-grey background on the whole screen,
// so the TUI looks the same whatever the terminal background is.
// VEGASYNCOR_THEME=terminal keeps the terminal's own background and colours.

var (
	themeBg   = lipgloss.Color("#1B2333") // dark blue-grey
	themeText = lipgloss.Color("#DCE3EE")
)

// themeOn reports whether the painted background is in use.
var themeOn bool

// themeBase is the escape sequence selecting the theme background and text colour
// for the current terminal colour profile ("" if the terminal has no colours).
var themeBase string

// setupTheme chooses the theme at startup.
func setupTheme() {
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
		l = sgrRe.ReplaceAllStringFunc(l, func(seq string) string {
			if resetsColours(sgrRe.FindStringSubmatch(seq)[1]) {
				return seq + themeBase
			}
			return seq
		})
		if fill := w - lipgloss.Width(l); fill > 0 {
			l += strings.Repeat(" ", fill)
		}
		lines[i] = themeBase + l + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
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
