package visualizers

import "math"

const (
	spiralRotRate   = 0.35  // rotation per unit bass
	spiralTimeDrift = 0.018 // slow time-based rotation
	spiralZoom      = 1.03  // zoom-in factor per frame
)

// NewMilkdropSpiral creates a Milkdrop preset that warps the framebuffer in a
// rotating spiral. Bass tightens the spiral; time adds a slow drift.
func NewMilkdropSpiral() *MilkdropPreset {
	return &MilkdropPreset{warp: spiralWarp}
}

func spiralWarp(nx, ny, t, bass, _ float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	theta += spiralRotRate*bass + spiralTimeDrift*t
	r *= spiralZoom

	return r * math.Cos(theta), r * math.Sin(theta)
}
