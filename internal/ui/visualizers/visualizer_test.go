package visualizers

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// --- Oscillogram tests ---

func TestOscillogram_ViewBeforeInit(t *testing.T) {
	o := NewOscillogram()
	got := o.View(0, 80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestOscillogram_InitZeroDuration(t *testing.T) {
	o := NewOscillogram()
	// Should not panic with durationMs=0
	o.Init("seed", 0)
	got := o.View(0, 80, 10)
	if got == "" {
		t.Error("View after Init(0) should return non-empty")
	}
}

func TestOscillogram_ViewDimensions(t *testing.T) {
	o := NewOscillogram()
	o.Init("test-seed", 30000)

	height := 10
	got := o.View(15000, 80, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Errorf("expected %d lines, got %d", height, len(lines))
	}
}

func TestOscillogram_ViewZeroDimensions(t *testing.T) {
	o := NewOscillogram()
	o.Init("seed", 10000)

	if got := o.View(0, 0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := o.View(0, 10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestOscillogram_ViewProgressBeyondDuration(t *testing.T) {
	o := NewOscillogram()
	o.Init("seed", 5000)
	// Should not panic when progressMs > durationMs
	got := o.View(99999, 40, 6)
	if got == "" {
		t.Error("should return non-empty even with progress beyond duration")
	}
}

func TestOscillogram_Deterministic(t *testing.T) {
	o1 := NewOscillogram()
	o1.Init("same-seed", 10000)
	v1 := o1.View(5000, 40, 6)

	o2 := NewOscillogram()
	o2.Init("same-seed", 10000)
	v2 := o2.View(5000, 40, 6)

	if v1 != v2 {
		t.Error("same seed should produce identical output")
	}
}

// --- Starfield tests ---

func TestStarfield_ViewBeforeInit(t *testing.T) {
	sf := NewStarfield()
	got := sf.View(0, 80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestStarfield_AdvanceBeforeInit(t *testing.T) {
	sf := NewStarfield()
	// Should not panic
	sf.Advance()
}

func TestStarfield_ViewDimensions(t *testing.T) {
	sf := NewStarfield()
	sf.Init("test-seed", 30000)

	height := 10
	got := sf.View(0, 80, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Errorf("expected %d lines, got %d", height, len(lines))
	}
}

func TestStarfield_ViewZeroDimensions(t *testing.T) {
	sf := NewStarfield()
	sf.Init("seed", 10000)

	if got := sf.View(0, 0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := sf.View(0, 10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestStarfield_AdvanceDoesNotPanic(t *testing.T) {
	sf := NewStarfield()
	sf.Init("seed", 10000)
	// Run many advances without panic
	for i := 0; i < 1000; i++ {
		sf.Advance()
	}
	got := sf.View(0, 40, 10)
	if got == "" {
		t.Error("should produce output after many advances")
	}
}

func TestStarfield_ResizeGrid(t *testing.T) {
	sf := NewStarfield()
	sf.Init("seed", 10000)

	// First render at one size
	sf.View(0, 40, 10)
	// Render at different size — should resize grid without panic
	got := sf.View(0, 80, 20)
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Errorf("expected 20 lines after resize, got %d", len(lines))
	}
}

// --- AlbumArt tests ---

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	return img
}

func TestAlbumArt_ViewBeforeInit(t *testing.T) {
	a := NewAlbumArt()
	got := a.View(0, 80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestAlbumArt_AdvanceBeforeInit(t *testing.T) {
	a := NewAlbumArt()
	// Should not panic
	a.Advance()
}

func TestAlbumArt_ViewZeroDimensions(t *testing.T) {
	a := NewAlbumArt()
	a.Init("seed", 10000)

	if got := a.View(0, 0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := a.View(0, 10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestAlbumArt_ViewDimensions(t *testing.T) {
	a := NewAlbumArt()
	a.Init("seed", 10000)
	a.SetImage(testImage(64, 64))

	height := 10
	got := a.View(0, 80, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Errorf("expected %d lines, got %d", height, len(lines))
	}
}

func TestAlbumArt_ResizeGrid(t *testing.T) {
	a := NewAlbumArt()
	a.Init("seed", 10000)
	a.SetImage(testImage(64, 64))

	a.View(0, 40, 10)
	got := a.View(0, 80, 20)
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Errorf("expected 20 lines after resize, got %d", len(lines))
	}
}

func TestAlbumArt_Deterministic(t *testing.T) {
	img := testImage(64, 64)

	a1 := NewAlbumArt()
	a1.Init("same-seed", 10000)
	a1.SetImage(img)
	for i := 0; i < 50; i++ {
		a1.Advance()
	}
	v1 := a1.View(0, 40, 10)

	a2 := NewAlbumArt()
	a2.Init("same-seed", 10000)
	a2.SetImage(img)
	for i := 0; i < 50; i++ {
		a2.Advance()
	}
	v2 := a2.View(0, 40, 10)

	if v1 != v2 {
		t.Error("same seed and image should produce identical output")
	}
}

func TestAlbumArt_AdvanceRespectsFrames(t *testing.T) {
	a := NewAlbumArt()
	a.Init("seed", 10000)
	a.SetImage(testImage(16, 16))

	// Advance without a View first — totalFrames is 0, should not resolve
	a.Advance()
	if a.resolved {
		t.Error("should not resolve before first View (totalFrames=0)")
	}

	// First View sets totalFrames via computeGrid
	a.View(0, 20, 10)

	// Advance should now increment frame
	a.Advance()
	if a.resolved {
		t.Error("should not resolve after 1 advance")
	}
}

func TestAlbumArt_ResolvesAfterEnoughAdvances(t *testing.T) {
	a := NewAlbumArt()
	a.Init("seed", 10000)
	a.SetImage(testImage(16, 16))
	a.View(0, 20, 10) // trigger computeGrid

	for i := 0; i < 200; i++ {
		a.Advance()
	}
	if !a.resolved {
		t.Error("should be resolved after 200 advances (totalFrames=150)")
	}
}

func TestAlbumArt_MusicNoteFallback(t *testing.T) {
	img := MusicNoteFallback()
	bounds := img.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Errorf("expected 16x16 fallback, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}
