package visualizers

import (
	"hash/fnv"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var upperBlocks = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// For the bottom half, we use standard block chars with swapped fg/bg.
// To fill top N/8 of a cell: use the block that fills bottom (8-N)/8 with fg=bgColor,
// and set bg=barColor. The "empty" foreground masks the bottom, bar color shows on top.
// Index 0 = fill top 1/8, index 6 = fill top 7/8.
var lowerMasks = [7]string{"▇", "▆", "▅", "▄", "▃", "▂", "▁"}

// pitchHues maps 12 pseudo-pitch classes to hue values.
var pitchHues = [12]float64{
	0, 30, 60, 90, 120, 150, 180, 210, 240, 270, 300, 330,
}

type Oscillogram struct {
	amplitudes []float64
	hues       []float64
}

type oscCol struct {
	amp        float64
	fr, fg, fb int // foreground RGB
}

func NewOscillogram() *Oscillogram {
	return &Oscillogram{}
}

func (o *Oscillogram) Init(seed string, durationMs int) {
	h := fnv.New64a()
	h.Write([]byte(seed))
	s := h.Sum64()

	count := durationMs / 100
	if count < 1 {
		count = 1
	}

	controlInterval := 8
	numControls := count/controlInterval + 2
	controls := make([]float64, numControls)
	hueControls := make([]float64, numControls)
	for i := range numControls {
		s = xorshift(s)
		controls[i] = float64(s%1000) / 1000.0
		s = xorshift(s)
		hueControls[i] = float64(s % 12)
	}

	o.amplitudes = make([]float64, count)
	o.hues = make([]float64, count)
	for i := range count {
		ci := i / controlInterval
		t := float64(i%controlInterval) / float64(controlInterval)
		mu := (1 - math.Cos(t*math.Pi)) / 2

		a := ci
		b := ci + 1
		if b >= numControls {
			b = numControls - 1
		}
		o.amplitudes[i] = controls[a]*(1-mu) + controls[b]*mu
		hi := hueControls[a]*(1-mu) + hueControls[b]*mu
		o.hues[i] = pitchHues[int(hi)%12]
	}
}

func (o *Oscillogram) Advance() {}

func (o *Oscillogram) View(progressMs, width, height int) string {
	if len(o.amplitudes) == 0 || width < 1 || height < 1 {
		return ""
	}

	centerIdx := progressMs / 100
	if centerIdx >= len(o.amplitudes) {
		centerIdx = len(o.amplitudes) - 1
	}

	halfW := width / 2

	// Precompute per-column data (amplitude + color).
	cols := make([]oscCol, width)
	for col := range width {
		segIdx := centerIdx - halfW + col
		if segIdx < 0 || segIdx >= len(o.amplitudes) {
			continue
		}
		amp := o.amplitudes[segIdx]
		hue := o.hues[segIdx]
		past := col < halfW

		sat := 0.6 + amp*0.4
		lum := 0.3 + amp*0.3
		if past {
			sat *= 0.4
			lum *= 0.6
		}
		r, g, b := hslToRGB(hue, sat, lum)
		cols[col] = oscCol{amp: amp, fr: r, fg: g, fb: b}
	}

	// Use both halves plus a center row for odd heights.
	halfH := height / 2
	if halfH < 1 {
		halfH = 1
	}

	// Resolve terminal background for bottom-half masking.
	var bgR, bgG, bgB int
	if lipgloss.HasDarkBackground() {
		bgR, bgG, bgB = 0, 0, 0
	} else {
		bgR, bgG, bgB = 255, 255, 255
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	// Top half (rows from top to center).
	for row := halfH - 1; row >= 0; row-- {
		for col := range width {
			c := cols[col]
			barHeight := c.amp * float64(halfH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				buf.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx > 7 {
				blockIdx = 7
			}
			buf.WriteString(ansiFg(c.fr, c.fg, c.fb))
			buf.WriteString(upperBlocks[blockIdx])
			buf.WriteString(ansiReset)
		}
		buf.WriteRune('\n')
	}

	// Center separator for odd heights.
	if height%2 == 1 {
		buf.WriteString(strings.Repeat(" ", width))
		buf.WriteRune('\n')
	}

	// Bottom half (mirror).
	for row := range halfH {
		for col := range width {
			c := cols[col]
			barHeight := c.amp * float64(halfH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				buf.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx >= 7 {
				buf.WriteString(ansiFg(c.fr, c.fg, c.fb))
				buf.WriteString("█")
				buf.WriteString(ansiReset)
			} else {
				buf.WriteString(ansiFgBg(bgR, bgG, bgB, c.fr, c.fg, c.fb))
				buf.WriteString(lowerMasks[blockIdx])
				buf.WriteString(ansiReset)
			}
		}
		if row < halfH-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}
