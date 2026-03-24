package visualizers

import (
	"hash/fnv"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
)

// Spectrum renders a classic spectrum analyzer with vertical bars and peak hold indicators.
type Spectrum struct {
	audioData *audio.FrequencyData
	prevBands [64]float32
	peakHold  [64]float32
	inited    bool
	rng       uint64 // seed for synthetic mode
	frame     int    // frame counter for synthetic animation
}

func NewSpectrum() *Spectrum {
	return &Spectrum{}
}

func (s *Spectrum) Init(seed string, durationMs int) {
	s.prevBands = [64]float32{}
	s.peakHold = [64]float32{}
	s.frame = 0
	h := fnv.New64a()
	h.Write([]byte(seed))
	s.rng = h.Sum64()
	s.inited = true
}

func (s *Spectrum) SetAudioData(data *audio.FrequencyData) {
	s.audioData = data
}

func (s *Spectrum) Advance() {
	s.frame++

	if s.audioData != nil {
		// Real audio mode: smooth toward actual data.
		for i := range 64 {
			target := s.audioData.Bands[i]
			if target > s.prevBands[i] {
				s.prevBands[i] = target
			} else {
				s.prevBands[i] *= 0.85
			}
			if s.prevBands[i] > s.peakHold[i] {
				s.peakHold[i] = s.prevBands[i]
			} else {
				s.peakHold[i] *= 0.97
			}
		}
	} else {
		// Synthetic mode: generate animated spectrum from seed.
		t := float64(s.frame) * 0.05
		for i := range 64 {
			fi := float64(i)
			// Layered sine waves at different frequencies, seeded by rng.
			phase := float64(s.rng>>uint(i%16)&0xFF) / 255.0 * math.Pi * 2
			val := 0.3*math.Sin(t*0.7+fi*0.15+phase) +
				0.2*math.Sin(t*1.3+fi*0.08+phase*1.5) +
				0.15*math.Sin(t*2.1+fi*0.22+phase*0.7)
			// Bias toward bass (lower bands louder).
			bassBoost := 1.0 - fi/64.0*0.5
			val = (val + 0.65) * bassBoost * 0.7
			if val < 0 {
				val = 0
			}
			if val > 1 {
				val = 1
			}
			target := float32(val)
			if target > s.prevBands[i] {
				s.prevBands[i] = target
			} else {
				s.prevBands[i] += (target - s.prevBands[i]) * 0.3
			}
			if s.prevBands[i] > s.peakHold[i] {
				s.peakHold[i] = s.prevBands[i]
			} else {
				s.peakHold[i] *= 0.97
			}
		}
	}
}

func (s *Spectrum) View(progressMs, width, height int) string {
	if !s.inited || width < 1 || height < 1 {
		return ""
	}

	var bgR, bgG, bgB int
	if lipgloss.HasDarkBackground() {
		bgR, bgG, bgB = 0, 0, 0
	} else {
		bgR, bgG, bgB = 255, 255, 255
	}

	var buf strings.Builder
	buf.Grow(width * height * 20)

	for row := range height {
		for col := range width {
			bandIdx := col * 64 / width
			if bandIdx >= 64 {
				bandIdx = 63
			}

			amp := float64(s.prevBands[bandIdx])
			peak := float64(s.peakHold[bandIdx])
			barHeight := amp * float64(height)
			peakRow := int(peak * float64(height))

			cellFromBottom := height - 1 - row
			cellLevel := barHeight - float64(cellFromBottom)

			// Hue: low freq = red, high freq = blue/purple.
			hue := float64(bandIdx) / 64.0 * 270.0
			sat := 0.8
			lum := 0.35 + amp*0.25

			if cellLevel > 0 {
				// Bar fill.
				blockIdx := int(cellLevel * 8)
				if blockIdx > 7 {
					blockIdx = 7
				}
				r, g, b := hslToRGB(hue, sat, lum)
				if blockIdx >= 7 {
					buf.WriteString(ansiFg(r, g, b))
					buf.WriteString("█")
					buf.WriteString(ansiReset)
				} else {
					buf.WriteString(ansiFgBg(r, g, b, bgR, bgG, bgB))
					buf.WriteString(upperBlocks[blockIdx])
					buf.WriteString(ansiReset)
				}
			} else if cellFromBottom == peakRow && peak > 0.02 {
				// Peak hold indicator.
				r, g, b := hslToRGB(hue, 0.9, 0.6)
				buf.WriteString(ansiFg(r, g, b))
				buf.WriteString("▁")
				buf.WriteString(ansiReset)
			} else {
				buf.WriteRune(' ')
			}
		}
		if row < height-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}
