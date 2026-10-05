package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResetsColours(t *testing.T) {
	cases := map[string]bool{
		"": true, "0": true, "39": true, "49": true, "1;0": true,
		"1":               false,
		"38;2;0;0;0":      false, // the zeros are colour components, not resets
		"48;5;0":          false,
		"38;2;10;20;30;0": true,
	}
	for in, want := range cases {
		if got := resetsColours(in); got != want {
			t.Errorf("resetsColours(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPaintBackground(t *testing.T) {
	themeOn, themeBase = true, "\x1b[48;2;27;35;51m"
	defer func() { themeOn, themeBase = false, "" }()
	out := paintBackground("ab\x1b[1mc\x1b[0md", 10, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, themeBase) || lipgloss.Width(l) != 10 {
			t.Errorf("line %d not painted to full width: %q", i, l)
		}
	}
	if !strings.Contains(lines[0], "\x1b[0m"+themeBase+"d") {
		t.Errorf("background not restored after reset: %q", lines[0])
	}
}
