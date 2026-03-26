package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// resolveHex extracts a plain hex string from an AdaptiveColor.
// Needed because go-colorful operates on raw hex values, not lipgloss types.
func resolveHex(ac lipgloss.AdaptiveColor) string {
	if lipgloss.HasDarkBackground() {
		return ac.Dark
	}
	return ac.Light
}

func renderProgressBar(width, progressMs, durationMs int) string {
	cur := formatDuration(time.Duration(progressMs) * time.Millisecond)
	remainMs := durationMs - progressMs
	if remainMs < 0 {
		remainMs = 0
	}
	total := "-" + formatDuration(time.Duration(remainMs)*time.Millisecond)

	// content width inside nowPlayingStyle (padding 0,1 = 2 chars horizontal)
	contentWidth := width - 2
	// bar width = content width minus timestamps and spacing: "0:00 ··· 0:00"
	barWidth := contentWidth - len(cur) - len(total) - 2
	if barWidth < 4 {
		return fmt.Sprintf("%s / %s", progressTimeStyle.Render(cur), progressTimeStyle.Render(total))
	}

	filled := 0
	if durationMs > 0 {
		filled = barWidth * progressMs / durationMs
	}
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	filledStr := renderGradientFill(
		filled,
		resolveHex(colorPrimary),
		resolveHex(colorTip),
	)
	bar := filledStr + progressEmptyStyle.Render(strings.Repeat("─", empty))

	return progressTimeStyle.Render(cur) + " " + bar + " " + progressTimeStyle.Render(total)
}

func renderGradientFill(filled int, solidHex, tipHex string) string {
	if filled <= 0 {
		return ""
	}

	solidStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(solidHex))
	tipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tipHex))

	solidLen := filled * 70 / 100
	gradLen := filled - solidLen

	if gradLen <= 1 {
		return solidStyle.Render(strings.Repeat("━", filled-1)) + tipStyle.Render("●")
	}

	cSolid, errS := colorful.Hex(solidHex)
	cTip, errT := colorful.Hex(tipHex)
	if errS != nil || errT != nil {
		return solidStyle.Render(strings.Repeat("━", filled-1)) + tipStyle.Render("●")
	}

	var b strings.Builder

	b.WriteString(solidStyle.Render(strings.Repeat("━", solidLen)))

	for i := range gradLen - 1 {
		t := float64(i+1) / float64(gradLen)
		c := cSolid.BlendHcl(cTip, t).Clamped()
		r, g, bl := c.RGB255()
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm━\x1b[0m", r, g, bl)
	}
	b.WriteString(tipStyle.Render("●"))

	return b.String()
}
