package visualizers

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/audio"
)

// --- Oscillogram tests ---

func TestOscillogram_ViewBeforeInit(t *testing.T) {
	o := NewOscillogram()
	got := o.View(0, 80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestOscillogram_AdvanceBeforeInit(t *testing.T) {
	o := NewOscillogram()
	// Should not panic.
	o.Advance()
}

func TestOscillogram_NoAudioShowsRestingBars(t *testing.T) {
	o := NewOscillogram()
	o.Init("seed", 10000)
	height := 10
	got := o.View(0, 80, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Errorf("expected %d lines, got %d", height, len(lines))
	}
	// Should contain ANSI color escapes from the gradient resting bars.
	if !strings.Contains(got, "\x1b[38;2;") {
		t.Error("no-audio view should contain colored resting bars")
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

func TestOscillogram_ViewDimensions(t *testing.T) {
	o := NewOscillogram()
	o.Init("seed", 10000)
	for _, height := range []int{1, 2, 3, 10, 21} {
		got := o.View(0, 40, height)
		lines := strings.Split(got, "\n")
		if len(lines) != height {
			t.Errorf("height=%d: expected %d lines, got %d", height, height, len(lines))
		}
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

// --- Spectrum tests ---

func TestSpectrum_ViewBeforeInit(t *testing.T) {
	s := NewSpectrum()
	got := s.View(0, 80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestSpectrum_AdvanceBeforeInit(t *testing.T) {
	s := NewSpectrum()
	// Should not panic.
	s.Advance()
}

func TestSpectrum_ViewZeroDimensions(t *testing.T) {
	s := NewSpectrum()
	s.Init("seed", 10000)

	if got := s.View(0, 0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := s.View(0, 10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestSpectrum_ViewDimensions(t *testing.T) {
	s := NewSpectrum()
	s.Init("seed", 10000)

	height := 10
	got := s.View(0, 80, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Errorf("expected %d lines, got %d", height, len(lines))
	}
}

func TestSpectrum_DecaysToZero(t *testing.T) {
	s := NewSpectrum()
	s.Init("seed", 10000)

	// Feed some audio data, then remove it and advance many times.
	s.SetAudioData(&audio.FrequencyData{
		Bands: [audio.NumBands]float32{0.5, 0.6, 0.7, 0.8},
		Peak:  0.8,
		Bass:  0.5,
	})
	s.Advance()

	// Remove audio and decay.
	s.SetAudioData(nil)
	for i := 0; i < 200; i++ {
		s.Advance()
	}

	// All bands should be near zero after enough decay.
	for i, v := range s.prevBands {
		if v > 0.001 {
			t.Errorf("prevBands[%d]=%.4f should be near zero after decay", i, v)
			break
		}
	}
}
