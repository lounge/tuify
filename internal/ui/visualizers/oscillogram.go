package visualizers

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
)

var upperBlocks = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// For the bottom half, we use standard block chars with swapped fg/bg.
var lowerMasks = [7]string{"▇", "▆", "▅", "▄", "▃", "▂", "▁"}

const (
	oscMinAmp      = 0.005  // resting bar height for the idle gradient line
	oscDecayActive = 0.82   // band release decay per tick when audio is present
	oscDecayIdle   = 0.88   // band decay per tick when no audio
)

type Oscillogram struct {
	bands  [audio.NumBands]float32 // smoothed band values
	inited bool
}

type oscCol struct {
	amp     float64
	r, g, b int
}

func NewOscillogram() *Oscillogram {
	return &Oscillogram{}
}

func (o *Oscillogram) Init(seed string, durationMs int) {
	o.bands = [audio.NumBands]float32{}
	o.inited = true
}

func (o *Oscillogram) SetAudioData(data *audio.FrequencyData) {
	if data != nil {
		for i := range audio.NumBands {
			target := data.Bands[i]
			if target > o.bands[i] {
				o.bands[i] = target // fast attack
			} else {
				o.bands[i] *= oscDecayActive
			}
		}
	} else {
		// Decay toward resting level.
		for i := range audio.NumBands {
			o.bands[i] *= oscDecayIdle
		}
	}
}

func (o *Oscillogram) Advance() {
	if !o.inited {
		return
	}
}

func (o *Oscillogram) View(progressMs, width, height int) string {
	if !o.inited || width < 1 || height < 1 {
		return ""
	}

	halfH := height / 2
	if halfH < 1 {
		halfH = 1
	}

	var bgR, bgG, bgB int
	if lipgloss.HasDarkBackground() {
		bgR, bgG, bgB = 0, 0, 0
	} else {
		bgR, bgG, bgB = 255, 255, 255
	}

	cols := make([]oscCol, width)
	for col := range width {
		bandIdx := col * audio.NumBands / width
		if bandIdx >= audio.NumBands {
			bandIdx = audio.NumBands - 1
		}
		amp := float64(o.bands[bandIdx])
		if amp < oscMinAmp {
			amp = oscMinAmp
		}
		hue := float64(bandIdx) / float64(audio.NumBands) * 300.0
		sat := 0.7 + amp*0.3
		lum := 0.25 + amp*0.35
		r, g, b := hslToRGB(hue, sat, lum)
		cols[col] = oscCol{amp: amp, r: r, g: g, b: b}
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	rowsWritten := 0

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
			buf.WriteString(ansiFg(c.r, c.g, c.b))
			buf.WriteString(upperBlocks[blockIdx])
			buf.WriteString(ansiReset)
		}
		rowsWritten++
		if rowsWritten < height {
			buf.WriteRune('\n')
		}
	}

	// Center separator for odd heights.
	if height%2 == 1 && rowsWritten < height {
		buf.WriteString(strings.Repeat(" ", width))
		rowsWritten++
		if rowsWritten < height {
			buf.WriteRune('\n')
		}
	}

	// Bottom half (mirror).
	for row := range halfH {
		if rowsWritten >= height {
			break
		}
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
				buf.WriteString(ansiFg(c.r, c.g, c.b))
				buf.WriteString("█")
				buf.WriteString(ansiReset)
			} else {
				buf.WriteString(ansiFgBg(bgR, bgG, bgB, c.r, c.g, c.b))
				buf.WriteString(lowerMasks[blockIdx])
				buf.WriteString(ansiReset)
			}
		}
		rowsWritten++
		if rowsWritten < height {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}
