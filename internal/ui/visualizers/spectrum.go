package visualizers

import (
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

const (
	specDecayActive   = float32(0.85) // band release decay per tick with audio
	specDecayPeak     = float32(0.97) // peak hold decay per tick with audio
	specDecayIdle     = float32(0.9)  // band decay per tick without audio
	specDecayPeakIdle = float32(0.95) // peak hold decay per tick without audio
)

// Spectrum renders a classic spectrum analyzer with vertical bars and peak hold indicators.
type Spectrum struct {
	audioData *audio.FrequencyData
	prevBands [audio.NumBands]float32
	peakHold  [audio.NumBands]float32
	inited    bool
}

func NewSpectrum() *Spectrum {
	return &Spectrum{}
}

func (s *Spectrum) Init(seed string, durationMs int) {
	s.prevBands = [audio.NumBands]float32{}
	s.peakHold = [audio.NumBands]float32{}
	s.inited = true
}

func (s *Spectrum) SetAudioData(data *audio.FrequencyData) {
	s.audioData = data
}

func (s *Spectrum) Advance() {
	if !s.inited {
		return
	}
	if s.audioData != nil {
		// Real audio mode: smooth toward actual data.
		for i := range audio.NumBands {
			target := s.audioData.Bands[i]
			if target > s.prevBands[i] {
				s.prevBands[i] = target
			} else {
				s.prevBands[i] *= specDecayActive
			}
			if s.prevBands[i] > s.peakHold[i] {
				s.peakHold[i] = s.prevBands[i]
			} else {
				s.peakHold[i] *= specDecayPeak
			}
		}
	} else {
		// No audio: decay to zero.
		for i := range audio.NumBands {
			s.prevBands[i] *= specDecayIdle
			s.peakHold[i] *= specDecayPeakIdle
		}
	}
}

func (s *Spectrum) View(width, height int) string {
	if !s.inited || width < 1 || height < 1 {
		return ""
	}

	bgR, bgG, bgB := termBG()

	var buf strings.Builder
	buf.Grow(width * height * 20)

	for row := range height {
		for col := range width {
			bandIdx := col * audio.NumBands / width
			if bandIdx >= audio.NumBands {
				bandIdx = audio.NumBands - 1
			}

			amp := float64(s.prevBands[bandIdx])
			peak := float64(s.peakHold[bandIdx])
			barHeight := amp * float64(height)
			peakRow := int(peak * float64(height))

			cellFromBottom := height - 1 - row
			cellLevel := barHeight - float64(cellFromBottom)

			hue := bandHue(bandIdx)
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
