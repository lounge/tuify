package visualizers

import (
	"testing"
)

func TestXorshift_Deterministic(t *testing.T) {
	a := xorshift(42)
	b := xorshift(42)
	if a != b {
		t.Errorf("xorshift not deterministic: %d != %d", a, b)
	}
}

func TestXorshift_DifferentSeeds(t *testing.T) {
	a := xorshift(1)
	b := xorshift(2)
	if a == b {
		t.Errorf("xorshift same output for different seeds: %d", a)
	}
}

func TestXorshift_NonZero(t *testing.T) {
	// xorshift should produce non-zero output for non-zero input
	s := uint64(1)
	for i := 0; i < 100; i++ {
		s = xorshift(s)
		if s == 0 {
			t.Fatalf("xorshift produced zero at iteration %d", i)
		}
	}
}

func TestHslToRGB_Red(t *testing.T) {
	// Pure red: H=0, S=1, L=0.5
	r, g, b := hslToRGB(0, 1, 0.5)
	if r != 255 || g != 0 || b != 0 {
		t.Errorf("hslToRGB(0, 1, 0.5) = (%d, %d, %d), want (255, 0, 0)", r, g, b)
	}
}

func TestHslToRGB_Green(t *testing.T) {
	// Pure green: H=120, S=1, L=0.5
	r, g, b := hslToRGB(120, 1, 0.5)
	if r != 0 || g != 255 || b != 0 {
		t.Errorf("hslToRGB(120, 1, 0.5) = (%d, %d, %d), want (0, 255, 0)", r, g, b)
	}
}

func TestHslToRGB_Blue(t *testing.T) {
	// Pure blue: H=240, S=1, L=0.5
	r, g, b := hslToRGB(240, 1, 0.5)
	if r != 0 || g != 0 || b != 255 {
		t.Errorf("hslToRGB(240, 1, 0.5) = (%d, %d, %d), want (0, 0, 255)", r, g, b)
	}
}

func TestHslToRGB_White(t *testing.T) {
	// White: any H, S=0, L=1
	r, g, b := hslToRGB(0, 0, 1)
	if r != 255 || g != 255 || b != 255 {
		t.Errorf("hslToRGB(0, 0, 1) = (%d, %d, %d), want (255, 255, 255)", r, g, b)
	}
}

func TestHslToRGB_Black(t *testing.T) {
	// Black: any H, S=0, L=0
	r, g, b := hslToRGB(0, 0, 0)
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("hslToRGB(0, 0, 0) = (%d, %d, %d), want (0, 0, 0)", r, g, b)
	}
}

func TestHslToRGB_NegativeHue(t *testing.T) {
	// Negative hue should wrap around
	r1, g1, b1 := hslToRGB(-60, 1, 0.5)
	r2, g2, b2 := hslToRGB(300, 1, 0.5)
	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Errorf("hslToRGB(-60) = (%d,%d,%d) != hslToRGB(300) = (%d,%d,%d)", r1, g1, b1, r2, g2, b2)
	}
}

func TestHslToRGB_HueWraps360(t *testing.T) {
	// 360 should equal 0
	r1, g1, b1 := hslToRGB(360, 1, 0.5)
	r2, g2, b2 := hslToRGB(0, 1, 0.5)
	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Errorf("hslToRGB(360) = (%d,%d,%d) != hslToRGB(0) = (%d,%d,%d)", r1, g1, b1, r2, g2, b2)
	}
}

func TestHslToRGB_OutputRange(t *testing.T) {
	// Test various hues to ensure output is always in [0, 255]
	for h := -720.0; h <= 720.0; h += 30 {
		for _, s := range []float64{0, 0.5, 1} {
			for _, l := range []float64{0, 0.25, 0.5, 0.75, 1} {
				r, g, b := hslToRGB(h, s, l)
				if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
					t.Errorf("hslToRGB(%v, %v, %v) = (%d, %d, %d) out of [0,255]", h, s, l, r, g, b)
				}
			}
		}
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 0, 0},
		{0, 0, 255, 0},
		{255, 0, 255, 255},
	}

	for _, tt := range tests {
		if got := clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}
