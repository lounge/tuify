package visualizers

import "math"

const (
	tunnelBaseDepth = 0.3  // depth at zero bass
	tunnelBassDepth = 0.2  // additional depth per unit bass
	tunnelEpsilon   = 0.1  // prevents division by zero at center
	tunnelMaxRadius = 1.8  // clamp ceiling for source radius
	tunnelRotSpeed  = 0.05 // time-based rotation speed
	tunnelMidRot    = 0.1  // additional rotation per unit mid
)

// NewMilkdropTunnel creates a Milkdrop preset using inverse-radius mapping to
// produce an infinite tunnel rushing toward the viewer.
func NewMilkdropTunnel() *MilkdropPreset {
	return &MilkdropPreset{warp: tunnelWarp}
}

func tunnelWarp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	depth := tunnelBaseDepth + bass*tunnelBassDepth
	sr := depth / (r + tunnelEpsilon)
	if sr > tunnelMaxRadius {
		sr = tunnelMaxRadius
	}

	theta += tunnelRotSpeed*t + mid*tunnelMidRot

	return sr * math.Cos(theta), sr * math.Sin(theta)
}
