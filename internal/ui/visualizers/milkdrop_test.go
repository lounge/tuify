package visualizers

import (
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/audio"
)

// --- helpers ---

type milkdropViz interface {
	Visualizer
	AudioAware
}

func testMilkdropViewBeforeInit(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	if got := v.View(80, 10); got != "" {
		t.Errorf("%s: View before Init should return empty, got len=%d", name, len(got))
	}
}

func testMilkdropAdvanceBeforeInit(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	v.Advance() // must not panic
}

func testMilkdropViewZero(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	v.Init("seed", 10000)
	if got := v.View(0, 10); got != "" {
		t.Errorf("%s: width=0 should return empty", name)
	}
	if got := v.View(10, 0); got != "" {
		t.Errorf("%s: height=0 should return empty", name)
	}
}

func testMilkdropViewDimensions(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	v.Init("seed", 10000)
	for _, height := range []int{1, 2, 5, 10} {
		got := v.View(40, height)
		lines := strings.Split(got, "\n")
		if len(lines) != height {
			t.Errorf("%s: height=%d: expected %d lines, got %d", name, height, height, len(lines))
		}
	}
}

func testMilkdropResize(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	v.Init("seed", 10000)
	v.View(40, 10)
	got := v.View(80, 20)
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Errorf("%s: expected 20 lines after resize, got %d", name, len(lines))
	}
}

func testMilkdropDecay(t *testing.T, v milkdropViz, name string) {
	t.Helper()
	v.Init("seed", 10000)
	v.View(20, 10) // trigger initial resize

	// Feed audio.
	v.SetAudioData(&audio.FrequencyData{
		Bands: [audio.NumBands]float32{0.8, 0.7, 0.6, 0.5},
		Peak:  0.8,
		Bass:  0.7,
		Mid:   0.5,
		High:  0.3,
	})
	for range 30 {
		v.Advance()
	}

	// Remove audio and let it decay.
	v.SetAudioData(nil)
	for range 300 {
		v.Advance()
	}

	// Check that the framebuffer has decayed to near-black.
	// Access the embedded base to inspect pixels.
	var base *milkdropBase
	switch vt := v.(type) {
	case *MilkdropSpiral:
		base = &vt.milkdropBase
	case *MilkdropTunnel:
		base = &vt.milkdropBase
	case *MilkdropKaleidoscope:
		base = &vt.milkdropBase
	case *MilkdropRipple:
		base = &vt.milkdropBase
	}
	if base == nil {
		t.Fatalf("%s: could not access milkdropBase", name)
	}
	for i, p := range base.fb {
		if p.l > 0.01 {
			t.Errorf("%s: pixel %d has l=%.4f, expected near zero after decay", name, i, p.l)
			break
		}
	}
}

// --- Spiral ---

func TestMilkdropSpiral_ViewBeforeInit(t *testing.T) {
	testMilkdropViewBeforeInit(t, NewMilkdropSpiral(), "Spiral")
}
func TestMilkdropSpiral_AdvanceBeforeInit(t *testing.T) {
	testMilkdropAdvanceBeforeInit(t, NewMilkdropSpiral(), "Spiral")
}
func TestMilkdropSpiral_ViewZeroDimensions(t *testing.T) {
	testMilkdropViewZero(t, NewMilkdropSpiral(), "Spiral")
}
func TestMilkdropSpiral_ViewDimensions(t *testing.T) {
	testMilkdropViewDimensions(t, NewMilkdropSpiral(), "Spiral")
}
func TestMilkdropSpiral_Resize(t *testing.T) {
	testMilkdropResize(t, NewMilkdropSpiral(), "Spiral")
}
func TestMilkdropSpiral_DecaysToBlack(t *testing.T) {
	testMilkdropDecay(t, NewMilkdropSpiral(), "Spiral")
}

// --- Tunnel ---

func TestMilkdropTunnel_ViewBeforeInit(t *testing.T) {
	testMilkdropViewBeforeInit(t, NewMilkdropTunnel(), "Tunnel")
}
func TestMilkdropTunnel_AdvanceBeforeInit(t *testing.T) {
	testMilkdropAdvanceBeforeInit(t, NewMilkdropTunnel(), "Tunnel")
}
func TestMilkdropTunnel_ViewZeroDimensions(t *testing.T) {
	testMilkdropViewZero(t, NewMilkdropTunnel(), "Tunnel")
}
func TestMilkdropTunnel_ViewDimensions(t *testing.T) {
	testMilkdropViewDimensions(t, NewMilkdropTunnel(), "Tunnel")
}
func TestMilkdropTunnel_Resize(t *testing.T) {
	testMilkdropResize(t, NewMilkdropTunnel(), "Tunnel")
}
func TestMilkdropTunnel_DecaysToBlack(t *testing.T) {
	testMilkdropDecay(t, NewMilkdropTunnel(), "Tunnel")
}

// --- Kaleidoscope ---

func TestMilkdropKaleidoscope_ViewBeforeInit(t *testing.T) {
	testMilkdropViewBeforeInit(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}
func TestMilkdropKaleidoscope_AdvanceBeforeInit(t *testing.T) {
	testMilkdropAdvanceBeforeInit(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}
func TestMilkdropKaleidoscope_ViewZeroDimensions(t *testing.T) {
	testMilkdropViewZero(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}
func TestMilkdropKaleidoscope_ViewDimensions(t *testing.T) {
	testMilkdropViewDimensions(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}
func TestMilkdropKaleidoscope_Resize(t *testing.T) {
	testMilkdropResize(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}
func TestMilkdropKaleidoscope_DecaysToBlack(t *testing.T) {
	testMilkdropDecay(t, NewMilkdropKaleidoscope(), "Kaleidoscope")
}

// --- Ripple ---

func TestMilkdropRipple_ViewBeforeInit(t *testing.T) {
	testMilkdropViewBeforeInit(t, NewMilkdropRipple(), "Ripple")
}
func TestMilkdropRipple_AdvanceBeforeInit(t *testing.T) {
	testMilkdropAdvanceBeforeInit(t, NewMilkdropRipple(), "Ripple")
}
func TestMilkdropRipple_ViewZeroDimensions(t *testing.T) {
	testMilkdropViewZero(t, NewMilkdropRipple(), "Ripple")
}
func TestMilkdropRipple_ViewDimensions(t *testing.T) {
	testMilkdropViewDimensions(t, NewMilkdropRipple(), "Ripple")
}
func TestMilkdropRipple_Resize(t *testing.T) {
	testMilkdropResize(t, NewMilkdropRipple(), "Ripple")
}
func TestMilkdropRipple_DecaysToBlack(t *testing.T) {
	testMilkdropDecay(t, NewMilkdropRipple(), "Ripple")
}
