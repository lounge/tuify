package visualizers

import (
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
