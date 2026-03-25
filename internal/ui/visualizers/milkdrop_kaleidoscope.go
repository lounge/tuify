package visualizers

import (
	"math"

	"github.com/lounge/tuify/internal/audio"
)

// MilkdropKaleidoscope folds the framebuffer into mirror-symmetric sectors.
// The number of segments shifts with bass energy, creating morphing flashes of
// symmetry.
type MilkdropKaleidoscope struct {
	milkdropBase
}

func NewMilkdropKaleidoscope() *MilkdropKaleidoscope {
	return &MilkdropKaleidoscope{}
}

func (v *MilkdropKaleidoscope) Init(seed string, durationMs int) { v.mdInit() }

func (v *MilkdropKaleidoscope) SetAudioData(data *audio.FrequencyData) { v.mdSetAudioData(data) }

func (v *MilkdropKaleidoscope) Advance() { v.advanceBase(v.warp) }

func (v *MilkdropKaleidoscope) View(w, h int) string {
	if !v.inited || w < 1 || h < 1 {
		return ""
	}
	v.resize(w, h)
	return v.render(w, h)
}

func (v *MilkdropKaleidoscope) warp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	// Number of mirror segments: 4–8, driven by bass.
	numSegs := 5 + bass*3
	if numSegs < 4 {
		numSegs = 4
	} else if numSegs > 8 {
		numSegs = 8
	}
	seg := math.Pi / numSegs

	// Rotate slowly over time, highs add extra spin.
	theta += 0.1*t + mid*0.2

	// Fold angle into one segment and mirror.
	theta = math.Mod(theta, 2*seg)
	if theta < 0 {
		theta += 2 * seg
	}
	if theta > seg {
		theta = 2*seg - theta
	}

	// Slight zoom for feedback.
	r *= 1.02

	return r * math.Cos(theta), r * math.Sin(theta)
}
