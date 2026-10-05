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

	sTitle    = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sMuted    = lipgloss.NewStyle().Foreground(cMuted)
	sBold     = lipgloss.NewStyle().Bold(true)
	sOK       = lipgloss.NewStyle().Foreground(cOK)
	sWarn     = lipgloss.NewStyle().Foreground(cWarn)
	sErr      = lipgloss.NewStyle().Foreground(cErr)
	sRun      = lipgloss.NewStyle().Foreground(cRun)
	sSel      = lipgloss.NewStyle().Background(cSelBg).Foreground(cText)
	sSection  = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sKey      = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sROBadge  = lipgloss.NewStyle().Foreground(cWarn).Background(cBadgeBg).Bold(true).Padding(0, 1)
	sTabOn    = lipgloss.NewStyle().Bold(true).Foreground(cText).Background(cSelBg).Padding(0, 1)
	sTabOff   = lipgloss.NewStyle().Foreground(cMuted).Padding(0, 1)
	sBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 1)
	sFocusBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
	sRule     = lipgloss.NewStyle().Foreground(cFaint)
	sClock    = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sCheck    = lipgloss.NewStyle().Bold(true).Foreground(cOK)
)

func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.Truncate(s, w, "…")
}

// truncLeft tronca mantenendo la parte finale (utile per i percorsi).
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

// statusTag è l'etichetta di stato a larghezza fissa (5 colonne, solo ASCII).
func statusTag(status string) string {
	switch status {
	case api.StatusOK:
		return sOK.Render("[OK] ")
	case api.StatusWarning:
		return sWarn.Render("[AVV]")
	case api.StatusError:
		return sErr.Render("[ERR]")
	case api.StatusCancelled:
		return sMuted.Render("[ANN]")
	case api.StatusSkipped:
		return sMuted.Render("[SAL]")
	case api.StatusRunning:
		return sRun.Render("[>>>]")
	}
	return sMuted.Render("[ - ]")
}

// statusStyle è il colore associato a un esito.
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

func statusLabel(status string) string {
	switch status {
	case api.StatusOK:
		return sOK.Render("completato")
	case api.StatusWarning:
		return sWarn.Render("con avvisi")
	case api.StatusError:
		return sErr.Render("errore")
	case api.StatusCancelled:
		return sMuted.Render("annullato")
	case api.StatusSkipped:
		return sMuted.Render("saltato")
	case api.StatusRunning:
		return sRun.Render("in corso")
	}
	return status
}

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
