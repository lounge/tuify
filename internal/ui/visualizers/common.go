package visualizers

import (
	"fmt"
	"image"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
)

type Visualizer interface {
	Init(seed string, durationMs int)
	Advance()
	View(width, height int) string
}

type ImageAware interface {
	SetImage(img image.Image)
}

// AudioAware is implemented by visualizers that consume real-time frequency data.
type AudioAware interface {
	SetAudioData(data *audio.FrequencyData)
}

// LyricsAware is implemented by visualizers that display lyrics.
type LyricsAware interface {
	SetLyrics(lines []string)
	SetInstrumental()
}

// ProgressAware is implemented by visualizers that need real-time playback progress.
type ProgressAware interface {
	SetProgress(progressMs int)
}

func xorshift(s uint64) uint64 {
	s ^= s << 13
	s ^= s >> 7
	s ^= s << 17
	return s
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func hslToRGB(h, s, l float64) (int, int, int) {
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

	return clamp(int((r+m)*255), 0, 255),
		clamp(int((g+m)*255), 0, 255),
		clamp(int((b+m)*255), 0, 255)
}

// ansiFg returns a direct ANSI 24-bit foreground escape for the given color.
func ansiFg(r, g, b int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// ansiFgBg returns ANSI 24-bit foreground + background escapes.
func ansiFgBg(fgR, fgG, fgB, bgR, bgG, bgB int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm", fgR, fgG, fgB, bgR, bgG, bgB)
}

const ansiReset = "\x1b[0m"

// Theme hue range — green (primary) to purple (secondary).
const (
	themeHueStart = 130.0
	themeHueRange = 150.0 // themeHueStart + themeHueRange = 280
)

// bandHue returns a hue for frequency band index, sweeping green → purple.
func bandHue(bandIdx int) float64 {
	return themeHueStart + float64(bandIdx)/float64(audio.NumBands)*themeHueRange
}

// BeatDetector uses spectral flux with an adaptive threshold to detect beats
// and estimate tempo. Embed in any visualizer that needs tempo-aware behavior.
type BeatDetector struct {
	prevBands  [audio.NumBands]float32 // previous frame's bands for flux calculation
	fluxAvg    float64                 // running average of spectral flux
	hasPrev    bool                    // whether prevBands is populated
	lastBeatMs int32
	cooldown   bool // true while flux is still above threshold after a beat
	intervals  []int32
	TempoMul   float64 // 0.4–1.6, maps BPM to speed multiplier
	Pulse      float64 // 1.0 on beat, decays toward 0
}

const (
	beatFluxMul    = 2.0  // flux must exceed average by this multiplier to trigger
	beatFluxAlpha  = 0.05 // EMA smoothing for running flux average
	beatCooldownMs = 200  // min ms between beats to avoid double-triggers
	beatMaxHistory = 8    // number of recent beat intervals to average
	beatPulseDecay = 0.85 // per-frame decay for Pulse
)

// Reset clears all beat state. Call on track change or Init.
func (bd *BeatDetector) Reset() {
	bd.prevBands = [audio.NumBands]float32{}
	bd.fluxAvg = 0
	bd.hasPrev = false
	bd.lastBeatMs = 0
	bd.cooldown = false
	bd.intervals = bd.intervals[:0]
	bd.TempoMul = 1.0
	bd.Pulse = 0
}

// Tick decays the pulse and processes a new frame of frequency bands.
// Call once per frame with the full band data and playback progress.
func (bd *BeatDetector) Tick(bands *[audio.NumBands]float32, progressMs int32) {
	bd.Pulse *= beatPulseDecay

	// Detect seek or track change.
	if bd.lastBeatMs > 0 && (progressMs < bd.lastBeatMs || progressMs-bd.lastBeatMs > 5000) {
		bd.Reset()
	}

	if !bd.hasPrev {
		bd.prevBands = *bands
		bd.hasPrev = true
		return
	}

	// Spectral flux: sum of half-wave rectified energy increases across bands.
	var flux float64
	for i := range audio.NumBands {
		diff := float64(bands[i]) - float64(bd.prevBands[i])
		if diff > 0 {
			flux += diff * diff
		}
	}
	bd.prevBands = *bands

	// Update running average with EMA.
	bd.fluxAvg = bd.fluxAvg*(1-beatFluxAlpha) + flux*beatFluxAlpha

	// Adaptive threshold: beat when flux exceeds running average by multiplier.
	threshold := bd.fluxAvg * beatFluxMul
	if threshold < 0.01 {
		threshold = 0.01 // minimum threshold for very quiet passages
	}

	above := flux > threshold
	if above && !bd.cooldown {
		bd.Pulse = 1.0
		bd.cooldown = true
		if bd.lastBeatMs > 0 {
			interval := progressMs - bd.lastBeatMs
			if interval >= beatCooldownMs && interval < 3000 {
				bd.intervals = append(bd.intervals, interval)
				if len(bd.intervals) > beatMaxHistory {
					bd.intervals = bd.intervals[1:]
				}
				bd.updateTempo()
			}
		}
		bd.lastBeatMs = progressMs
	} else if !above {
		bd.cooldown = false
	}
}

func (bd *BeatDetector) updateTempo() {
	if len(bd.intervals) < 3 {
		return
	}
	var sum int64
	for _, iv := range bd.intervals {
		sum += int64(iv)
	}
	avgMs := float64(sum) / float64(len(bd.intervals))
	bpm := 60000.0 / avgMs

	bd.TempoMul = bpm / 120.0
	if bd.TempoMul < 0.4 {
		bd.TempoMul = 0.4
	}
	if bd.TempoMul > 1.6 {
		bd.TempoMul = 1.6
	}
}

// upperBlocks are ascending block-fill characters used by spectrum and oscillogram.
var upperBlocks = [8]string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// termBG returns the terminal background color as RGB.
func termBG() (int, int, int) {
	if lipgloss.HasDarkBackground() {
		return 0, 0, 0
	}
	return 255, 255, 255
}

// lerpAngle interpolates two hue values (0–360) taking the shortest arc.
func lerpAngle(a, b, t float64) float64 {
	diff := b - a
	if diff > 180 {
		diff -= 360
	} else if diff < -180 {
		diff += 360
	}
	h := a + diff*t
	if h < 0 {
		h += 360
	} else if h >= 360 {
		h -= 360
	}
	return h
}
