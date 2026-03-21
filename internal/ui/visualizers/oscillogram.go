package visualizers

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
)

// Block characters for the upper half, indexed 0–7 (smallest to full).
var upperBlocks = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// Block characters for the lower half (mirrored).
var lowerBlocks = [8]string{"▔", "▔", "🬂", "▀", "🬎", "🬎", "🬤", "█"}

type Oscillogram struct{}

func NewOscillogram() *Oscillogram {
	return &Oscillogram{}
}

func (o *Oscillogram) View(capture *audio.Capture, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}

	// How many samples we need: enough to fill the terminal width.
	// Each column represents a group of samples. Show ~2 seconds of audio.
	samplesPerCol := 44100 * 2 / width
	if samplesPerCol < 1 {
		samplesPerCol = 1
	}
	totalSamples := samplesPerCol * width
	samples := capture.Snapshot(totalSamples)

	// Compute per-column peak amplitude (envelope).
	type colData struct {
		peak float64 // 0..1 peak absolute amplitude
	}
	cols := make([]colData, width)
	for col := 0; col < width; col++ {
		start := col * samplesPerCol
		end := start + samplesPerCol
		if end > len(samples) {
			end = len(samples)
		}
		var maxAbs float64
		for i := start; i < end; i++ {
			a := math.Abs(float64(samples[i]))
			if a > maxAbs {
				maxAbs = a
			}
		}
		cols[col] = colData{peak: maxAbs}
	}

	// Find the global max for normalization.
	var globalMax float64
	for _, c := range cols {
		if c.peak > globalMax {
			globalMax = c.peak
		}
	}
	if globalMax < 0.001 {
		globalMax = 0.001
	}

	halfH := height / 2
	if halfH < 1 {
		halfH = 1
	}

	var b strings.Builder

	// Render top half (rows from top to center).
	for row := halfH - 1; row >= 0; row-- {
		for col := 0; col < width; col++ {
			amp := cols[col].peak / globalMax
			barHeight := amp * float64(halfH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				b.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx > 7 {
				blockIdx = 7
			}

			// Color: amplitude maps to hue (blue → green → yellow).
			hue := 200 - amp*140 // 200 (blue) down to 60 (yellow)
			sat := 0.6 + amp*0.4
			lum := 0.3 + amp*0.35
			// Dim older samples (left side).
			fade := 0.4 + 0.6*float64(col)/float64(width)
			sat *= fade
			lum *= fade

			color := hslToHex(hue, sat, lum)
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			b.WriteString(style.Render(upperBlocks[blockIdx]))
		}
		if row > 0 {
			b.WriteRune('\n')
		}
	}

	b.WriteRune('\n')

	// Render bottom half (mirror).
	for row := 0; row < halfH; row++ {
		for col := 0; col < width; col++ {
			amp := cols[col].peak / globalMax
			barHeight := amp * float64(halfH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				b.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx > 7 {
				blockIdx = 7
			}
			mirrorIdx := 7 - blockIdx

			hue := 200 - amp*140
			sat := 0.6 + amp*0.4
			lum := 0.3 + amp*0.35
			fade := 0.4 + 0.6*float64(col)/float64(width)
			sat *= fade
			lum *= fade

			color := hslToHex(hue, sat, lum)
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			b.WriteString(style.Render(lowerBlocks[mirrorIdx]))
		}
		if row < halfH-1 {
			b.WriteRune('\n')
		}
	}

	return b.String()
}

func hslToHex(h, s, l float64) string {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	ri := int((r + m) * 255)
	gi := int((g + m) * 255)
	bi := int((b + m) * 255)
	return fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
}
