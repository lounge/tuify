package visualizers

import "math"

const (
	kaleidoBaseSegs = 5.0  // base number of mirror segments
	kaleidoBassSegs = 3.0  // additional segments per unit bass
	kaleidoMinSegs  = 4.0  // minimum segment count
	kaleidoMaxSegs  = 8.0  // maximum segment count
	kaleidoRotSpeed = 0.1  // time-based rotation speed
	kaleidoMidRot   = 0.2  // additional rotation per unit mid
	kaleidoZoom     = 1.02 // zoom factor per frame
)

// NewMilkdropKaleidoscope creates a Milkdrop preset that folds the framebuffer
// into mirror-symmetric sectors. Bass shifts the number of segments.
func NewMilkdropKaleidoscope() *MilkdropPreset {
	return &MilkdropPreset{warp: kaleidoscopeWarp}
}

func kaleidoscopeWarp(nx, ny, t, bass, mid float64) (float64, float64) {
	r := math.Sqrt(nx*nx + ny*ny)
	theta := math.Atan2(ny, nx)

	numSegs := kaleidoBaseSegs + bass*kaleidoBassSegs
	if numSegs < kaleidoMinSegs {
		numSegs = kaleidoMinSegs
	} else if numSegs > kaleidoMaxSegs {
		numSegs = kaleidoMaxSegs
	}
	seg := math.Pi / numSegs

	theta += kaleidoRotSpeed*t + mid*kaleidoMidRot

	// Fold angle into one segment and mirror.
	theta = math.Mod(theta, 2*seg)
	if theta < 0 {
		theta += 2 * seg
	}
	if theta > seg {
		theta = 2*seg - theta
	}

	r *= kaleidoZoom

	return r * math.Cos(theta), r * math.Sin(theta)
}
