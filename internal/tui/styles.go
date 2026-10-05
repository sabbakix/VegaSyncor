package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"vegasyncor/internal/api"
)

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#1D5FBF", Dark: "#5FA8FF"}
	cMuted   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B93A1"}
	cFaint   = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#3A3F4B"}
	cOK      = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	cWarn    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	cErr     = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cRun     = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	cSelBg   = lipgloss.AdaptiveColor{Light: "#DBEAFE", Dark: "#1E3A5F"}
	cText    = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	cBadgeBg = lipgloss.AdaptiveColor{Light: "#FEF3C7", Dark: "#4A3A10"}
)

// styles, built from the palette by rebuildStyles (again after the theme changes it)
var (
	sTitle, sMuted, sBold, sOK, sWarn, sErr, sRun, sSel, sSection, sKey, sROBadge, sTabOn, sTabOff, sBox, sFocusBox, sRule, sClock, sSep, sCheck lipgloss.Style
)

func init() { rebuildStyles() }

func rebuildStyles() {
	sTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sMuted = lipgloss.NewStyle().Foreground(cMuted)
	sBold = lipgloss.NewStyle().Bold(true)
	sOK = lipgloss.NewStyle().Foreground(cOK)
	sWarn = lipgloss.NewStyle().Foreground(cWarn)
	sErr = lipgloss.NewStyle().Foreground(cErr)
	sRun = lipgloss.NewStyle().Foreground(cRun)
	sSel = lipgloss.NewStyle().Background(cSelBg).Foreground(cText)
	sSection = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sKey = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sROBadge = lipgloss.NewStyle().Foreground(cWarn).Background(cBadgeBg).Bold(true).Padding(0, 1)
	sTabOn = lipgloss.NewStyle().Bold(true).Foreground(cText).Background(cSelBg).Padding(0, 1)
	sTabOff = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
	sBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 1)
	sFocusBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	sRule = lipgloss.NewStyle().Foreground(cFaint)
	sClock = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sSep = lipgloss.NewStyle().Foreground(cMuted)
	sCheck = lipgloss.NewStyle().Bold(true).Foreground(cOK)
}

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "…")
}

// truncLeft truncates keeping the final part (useful for paths).
func truncLeft(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	for i := range r {
		if runewidth.StringWidth(string(r[i:])) <= w-1 {
			return "…" + string(r[i:])
		}
	}
	return "…"
}

func pad(s string, w int) string {
	return runewidth.FillRight(trunc(s, w), w)
}

// statusTag is the fixed-width status tag (5 columns, ASCII only).
func statusTag(status string) string {
	switch status {
	case api.StatusOK:
		return sOK.Render("[OK] ")
	case api.StatusWarning:
		return sWarn.Render(T("[WRN]"))
	case api.StatusError:
		return sErr.Render("[ERR]")
	case api.StatusCancelled:
		return sMuted.Render(T("[CAN]"))
	case api.StatusSkipped:
		return sMuted.Render(T("[SKP]"))
	case api.StatusRunning:
		return sRun.Render("[>>>]")
	}
	return sMuted.Render("[ - ]")
}

// statusStyle is the colour of an outcome.
func statusStyle(status string) lipgloss.Style {
	switch status {
	case api.StatusOK:
		return sOK
	case api.StatusWarning:
		return sWarn
	case api.StatusError:
		return sErr
	case api.StatusRunning:
		return sRun
	}
	return sMuted
}

// statusText is the translated name of an outcome.
func statusText(status string) string {
	switch status {
	case api.StatusOK:
		return T("completed")
	case api.StatusWarning:
		return T("with warnings")
	case api.StatusError:
		return T("error")
	case api.StatusCancelled:
		return T("cancelled")
	case api.StatusSkipped:
		return T("skipped")
	case api.StatusRunning:
		return T("running")
	}
	return status
}

func statusLabel(status string) string { return statusStyle(status).Render(statusText(status)) }

func progressBar(pct, width int) string {
	if width < 4 {
		return ""
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	full := pct * width / 100
	return sRun.Render(strings.Repeat("█", full)) + sRule.Render(strings.Repeat("░", width-full))
}

func rule(w int) string { return sRule.Render(strings.Repeat("─", max(w, 0))) }
