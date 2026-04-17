package visualizers

import (
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

const (
	oscMinAmp      = 0.005         // resting bar height for the idle gradient line
	oscDecayActive = float32(0.82) // band release decay per tick when audio is present
	oscDecayIdle   = float32(0.88) // band decay per tick when no audio
)

type Oscillogram struct {
	audioData *audio.FrequencyData
	bands     [audio.NumBands]float32 // smoothed band values
	inited    bool
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
	o.audioData = data
}

func (o *Oscillogram) Advance() {
	if !o.inited {
		return
	}
	if o.audioData != nil {
		for i := range audio.NumBands {
			target := o.audioData.Bands[i]
			if target > o.bands[i] {
				o.bands[i] = target // fast attack
			} else {
				o.bands[i] *= oscDecayActive
			}
		}
	} else {
		for i := range audio.NumBands {
			o.bands[i] *= oscDecayIdle
		}
	}
}

func (o *Oscillogram) View(width, height int) string {
	if !o.inited || width < 1 || height < 1 {
		return ""
	}

	topH := (height + 1) / 2
	botH := height / 2

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
		hue := bandHue(bandIdx)
		sat := 0.7 + amp*0.3
		lum := 0.25 + amp*0.35
		r, g, b := hslToRGB(hue, sat, lum)
		cols[col] = oscCol{amp: amp, r: r, g: g, b: b}
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	rowsWritten := 0

	// Top half (rows from top to center).
	for row := topH - 1; row >= 0; row-- {
		for col := range width {
			c := cols[col]
			barHeight := c.amp * float64(topH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				buf.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx > 7 {
				blockIdx = 7
			}
			writeAnsiFg(&buf, c.r, c.g, c.b)
			buf.WriteString(upperBlocks[blockIdx])
			buf.WriteString(ansiReset)
		}
		rowsWritten++
		if rowsWritten < height {
			buf.WriteRune('\n')
		}
	}

	// Bottom half (mirror).
	for row := range botH {
		if rowsWritten >= height {
			break
		}
		for col := range width {
			c := cols[col]
			barHeight := c.amp * float64(botH)
			cellLevel := barHeight - float64(row)

			if cellLevel <= 0 {
				buf.WriteRune(' ')
				continue
			}

			blockIdx := int(cellLevel * 8)
			if blockIdx > 7 {
				blockIdx = 7
			}
			writeAnsiFg(&buf, c.r, c.g, c.b)
			if blockIdx >= 7 {
				buf.WriteString("█")
			} else {
				buf.WriteString("\x1b[7m")
				buf.WriteString(upperBlocks[6-blockIdx])
			}
			buf.WriteString(ansiReset)
		}
		rowsWritten++
		if rowsWritten < height {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}
