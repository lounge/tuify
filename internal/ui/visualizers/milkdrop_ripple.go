package visualizers

import "math"

const (
	rippleBaseAmp   = 0.05 // wave amplitude at zero bass
	rippleBassAmp   = 0.08 // additional amplitude per unit bass
	rippleBaseFreq  = 12.0 // radial wave frequency at zero mid
	rippleMidFreq   = 8.0  // additional frequency per unit mid
	rippleBaseSpeed = 2.0  // wave propagation speed at zero energy
	rippleSpeedMul  = 3.0  // speed multiplier for audio energy
)

// NewMilkdropRipple creates a Milkdrop preset that displaces pixels along
// radial sine waves, creating expanding concentric ripples.
func NewMilkdropRipple() *MilkdropPreset {
	return &MilkdropPreset{warp: rippleWarp}
}

func rippleWarp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	amp := rippleBaseAmp + bass*rippleBassAmp
	freq := rippleBaseFreq + mid*rippleMidFreq
	speed := rippleBaseSpeed + (bass*mdEnergyBass+mid*mdEnergyMid)*rippleSpeedMul
	disp := amp * math.Sin(r*freq-t*speed)

	return nx + math.Cos(theta)*disp, ny + math.Sin(theta)*disp
}
