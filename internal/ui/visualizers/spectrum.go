package visualizers

import (
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
}

func NewSpectrum() *Spectrum {
	return &Spectrum{}
}

func (s *Spectrum) Init(seed string, durationMs int) {
	s.prevBands = [64]float32{}
	s.peakHold = [64]float32{}
	s.inited = true
}

func (s *Spectrum) SetAudioData(data *audio.FrequencyData) {
	s.audioData = data
}

func (s *Spectrum) Advance() {
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
		// No audio: decay to zero.
		for i := range 64 {
			s.prevBands[i] *= 0.9
			s.peakHold[i] *= 0.95
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
