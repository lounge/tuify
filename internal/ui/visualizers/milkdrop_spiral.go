package visualizers

import (
	"math"

	"github.com/lounge/tuify/internal/audio"
)

// MilkdropSpiral warps the framebuffer in a rotating spiral that tightens with
// bass energy. The feedback loop produces flowing trails that curl into the center.
type MilkdropSpiral struct {
	milkdropBase
}

func NewMilkdropSpiral() *MilkdropSpiral {
	return &MilkdropSpiral{}
}

func (v *MilkdropSpiral) Init(seed string, durationMs int) { v.mdInit() }

func (v *MilkdropSpiral) SetAudioData(data *audio.FrequencyData) { v.mdSetAudioData(data) }

func (v *MilkdropSpiral) Advance() { v.advanceBase(v.warp) }

func (v *MilkdropSpiral) View(w, h int) string {
	if !v.inited || w < 1 || h < 1 {
		return ""
	}
	v.resize(w, h)
	return v.render(w, h)
}

func (v *MilkdropSpiral) warp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	// Bass drives rotation speed, time adds slow drift.
	theta += 0.35*bass + 0.018*t
	// Slight zoom-in pulls content toward center, feeding the spiral.
	r *= 1.03

	return r * math.Cos(theta), r * math.Sin(theta)
}
