package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// adaptiveGradient holds both variants of a two-colour gradient, parsed
// once in RebuildStyles so renders don't re-parse hex every frame. Both
// variants are kept because which one applies depends on the terminal
// background, which lipgloss detects lazily on first use.
type adaptiveGradient struct {
	light, dark gradientEnds
}

// gradientEnds are the parsed endpoints of one gradient variant. ok is
// false when either hex failed to parse; from/to are then black, which is
// what colorful.Hex returns on error.
type gradientEnds struct {
	from, to colorful.Color
	ok       bool
}

func newAdaptiveGradient(from, to lipgloss.AdaptiveColor) adaptiveGradient {
	return adaptiveGradient{
		light: parseGradientEnds(from.Light, to.Light),
		dark:  parseGradientEnds(from.Dark, to.Dark),
	}
}

func parseGradientEnds(from, to string) gradientEnds {
	f, errF := colorful.Hex(from)
	t, errT := colorful.Hex(to)
	return gradientEnds{from: f, to: t, ok: errF == nil && errT == nil}
}

// current returns the variant for the terminal's background.
func (g adaptiveGradient) current() gradientEnds {
	if lipgloss.HasDarkBackground() {
		return g.dark
	}
	return g.light
}

func renderProgressBar(width, progressMs, durationMs int) string {
	cur := formatDuration(time.Duration(progressMs) * time.Millisecond)
	remainMs := max(durationMs-progressMs, 0)
	total := "-" + formatDuration(time.Duration(remainMs)*time.Millisecond)

	contentWidth := width - nowPlayingPadding
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
	if filled == 0 && progressMs > 0 {
		filled = 1
	}
	empty := barWidth - filled

	filledStr := renderGradientFill(filled)
	bar := filledStr + progressEmptyStyle.Render(strings.Repeat("─", empty))

	return progressTimeStyle.Render(cur) + " " + bar + " " + progressTimeStyle.Render(total)
}

func renderMiniBar(barWidth, progressMs, durationMs int) string {
	filled := 0
	if durationMs > 0 {
		filled = barWidth * progressMs / durationMs
	}
	if filled > barWidth {
		filled = barWidth
	}
	if filled == 0 && progressMs > 0 {
		filled = 1
	}
	empty := barWidth - filled

	filledStr := renderGradientFill(filled)
	return filledStr + progressEmptyStyle.Render(strings.Repeat("─", empty))
}

const progressSolidPct = 70 // percentage of the filled bar that uses the solid color

func renderGradientFill(filled int) string {
	if filled <= 0 {
		return ""
	}

	solidLen := filled * progressSolidPct / 100
	gradLen := filled - solidLen

	ends := progressGradient.current()
	if gradLen <= 1 || !ends.ok {
		return progressSolidStyle.Render(strings.Repeat("━", filled-1)) + progressTipStyle.Render("●")
	}

	var b strings.Builder
	b.Grow(solidLen*3 + 32 + (gradLen-1)*26)

	b.WriteString(progressSolidStyle.Render(strings.Repeat("━", solidLen)))

	var escBuf [24]byte
	for i := range gradLen - 1 {
		t := float64(i+1) / float64(gradLen)
		c := ends.from.BlendHcl(ends.to, t).Clamped()
		r, g, bl := c.RGB255()
		b.Write(appendRGBEscape(escBuf[:0], "\x1b[38;2;", r, g, bl))
		b.WriteString("━\x1b[0m")
	}
	b.WriteString(progressTipStyle.Render("●"))

	return b.String()
}

// appendRGBEscape appends prefix + "R;G;Bm" to dst, for 24-bit SGR colour
// escapes, without the per-component allocations of fmt or strconv.Itoa.
func appendRGBEscape(dst []byte, prefix string, r, g, b uint8) []byte {
	dst = append(dst, prefix...)
	dst = strconv.AppendUint(dst, uint64(r), 10)
	dst = append(dst, ';')
	dst = strconv.AppendUint(dst, uint64(g), 10)
	dst = append(dst, ';')
	dst = strconv.AppendUint(dst, uint64(b), 10)
	return append(dst, 'm')
}
