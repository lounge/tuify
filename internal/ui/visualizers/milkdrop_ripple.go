package visualizers

import (
	"math"

	"github.com/lounge/tuify/internal/audio"
)

// MilkdropRipple displaces pixels along radial sine waves, creating expanding
// concentric ripples. Bass amplifies the ripple intensity; mids increase
// frequency density.
type MilkdropRipple struct {
	milkdropBase
}

func NewMilkdropRipple() *MilkdropRipple {
	return &MilkdropRipple{}
}

func (v *MilkdropRipple) Init(seed string, durationMs int) { v.mdInit() }

func (v *MilkdropRipple) SetAudioData(data *audio.FrequencyData) { v.mdSetAudioData(data) }

func (v *MilkdropRipple) Advance() { v.advanceBase(v.warp) }

func (v *MilkdropRipple) View(w, h int) string {
	if !v.inited || w < 1 || h < 1 {
		return ""
	}
	v.resize(w, h)
	return v.render(w, h)
}

func (v *MilkdropRipple) warp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	// Radial sine wave displacement.
	amp := 0.05 + bass*0.08
	freq := 12.0 + mid*8.0
	speed := 2.0 + (bass*0.5+mid*0.3)*3.0
	disp := amp * math.Sin(r*freq-t*speed)

	sx := nx + math.Cos(theta)*disp
	sy := ny + math.Sin(theta)*disp
	return sx, sy
}
