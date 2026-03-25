package visualizers

import (
	"math"

	"github.com/lounge/tuify/internal/audio"
)

// MilkdropTunnel warps the framebuffer using an inverse-radius mapping to create
// an infinite tunnel rushing toward the viewer. Bass deepens the tunnel; mids add
// a slow twist.
type MilkdropTunnel struct {
	milkdropBase
}

func NewMilkdropTunnel() *MilkdropTunnel {
	return &MilkdropTunnel{}
}

func (v *MilkdropTunnel) Init(seed string, durationMs int) { v.mdInit() }

func (v *MilkdropTunnel) SetAudioData(data *audio.FrequencyData) { v.mdSetAudioData(data) }

func (v *MilkdropTunnel) Advance() { v.advanceBase(v.warp) }

func (v *MilkdropTunnel) View(w, h int) string {
	if !v.inited || w < 1 || h < 1 {
		return ""
	}
	v.resize(w, h)
	return v.render(w, h)
}

func (v *MilkdropTunnel) warp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	// Inverse radius: center maps to far, edges map to near.
	depth := 0.3 + bass*0.2
	sr := depth / (r + 0.1)
	// Clamp to prevent wild source coordinates.
	if sr > 1.8 {
		sr = 1.8
	}

	// Slow rotation driven by time and mids.
	theta += 0.05*t + mid*0.1

	return sr * math.Cos(theta), sr * math.Sin(theta)
}
