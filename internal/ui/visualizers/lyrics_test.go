package visualizers

import (
	"strings"
	"testing"
)

func TestLyrics_ViewBeforeInit(t *testing.T) {
	l := NewLyrics()
	got := l.View(80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestLyrics_ViewZeroDimensions(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)

	if got := l.View(0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := l.View(10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestLyrics_Loading(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)

	got := l.View(80, 10)
	if !strings.Contains(got, "Loading lyrics") {
		t.Errorf("expected loading message, got %q", got)
	}
}

func TestLyrics_NoLyrics(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(nil)

	got := l.View(80, 10)
	if !strings.Contains(got, "No lyrics found") {
		t.Errorf("expected no lyrics message, got %q", got)
	}
}

func TestLyrics_Instrumental(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetInstrumental()

	got := l.View(80, 10)
	if !strings.Contains(got, "Instrumental") {
		t.Errorf("expected instrumental message, got %q", got)
	}
}

func TestLyrics_SetLyricsClearsLoading(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics([]string{"Line one", "Line two"})

	got := l.View(80, 10)
	if strings.Contains(got, "Loading") {
		t.Error("should not show loading after SetLyrics")
	}
}

func TestLyrics_SetInstrumentalClearsLoading(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetInstrumental()

	if l.loading {
		t.Error("loading should be false after SetInstrumental")
	}
	if !l.instrumental {
		t.Error("instrumental should be true")
	}
}

func TestLyrics_ViewDimensions(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "Lyric line"
	}
	l.SetLyrics(lines)

	height := 10
	got := l.View(80, height)
	outputLines := strings.Split(got, "\n")
	if len(outputLines) != height {
		t.Errorf("expected %d lines, got %d", height, len(outputLines))
	}
}

func TestLyrics_ProgressScrolls(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "Line " + string(rune('A'+i))
	}
	l.SetLyrics(lines)

	// At progress 0, should show beginning
	l.SetProgress(0)
	v1 := l.View(80, 5)

	// At progress near end, should show end
	l.SetProgress(9500)
	v2 := l.View(80, 5)

	if v1 == v2 {
		t.Error("different progress positions should produce different views")
	}
}

func TestLyrics_AdvanceIsNoop(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics([]string{"test"})

	// Advance should not panic or change state
	l.Advance()
}

func TestLyrics_ReinitResetsState(t *testing.T) {
	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics([]string{"old lyrics"})

	// Re-init should reset to loading
	l.Init("new-seed", 20000)
	if !l.loading {
		t.Error("should be loading after re-init")
	}
	if l.lines != nil {
		t.Error("lines should be nil after re-init")
	}
}

func TestLyricGray_Dark(t *testing.T) {
	// Non-section, close distance
	g := lyricGray(true, false, 0)
	if g < 50 || g > 255 {
		t.Errorf("dark non-section dist=0: got %d", g)
	}

	// Should decrease with distance
	g1 := lyricGray(true, false, 1)
	g5 := lyricGray(true, false, 5)
	if g5 >= g1 {
		t.Errorf("dark gray should decrease with distance: dist=1 %d, dist=5 %d", g1, g5)
	}

	// Section markers are dimmer
	gs := lyricGray(true, true, 0)
	gn := lyricGray(true, false, 0)
	if gs >= gn {
		t.Errorf("dark section should be dimmer: section=%d, non-section=%d", gs, gn)
	}
}

func TestLyricGray_Light(t *testing.T) {
	// Light mode: grays should increase with distance (darker = lower number)
	g0 := lyricGray(false, false, 0)
	g5 := lyricGray(false, false, 5)
	if g5 <= g0 {
		t.Errorf("light gray should increase with distance: dist=0 %d, dist=5 %d", g0, g5)
	}
}

func TestCenterPad(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  int // expected length
	}{
		{"hello", 20, 20},
		{"hello", 5, 5},
		{"hello", 3, 5}, // doesn't truncate, just returns as-is
	}
	for _, tt := range tests {
		got := centerPad(tt.s, tt.width)
		if len(got) < len(tt.s) {
			t.Errorf("centerPad(%q, %d): result shorter than input", tt.s, tt.width)
		}
		if tt.width > len(tt.s) {
			// Should be centered (left-padded with spaces)
			trimmed := strings.TrimLeft(got, " ")
			if trimmed != tt.s {
				t.Errorf("centerPad(%q, %d): trimmed result %q != original", tt.s, tt.width, trimmed)
			}
		}
	}
}

func TestCenterPad_Empty(t *testing.T) {
	got := centerPad("", 10)
	// centerPad uses fmt.Sprintf("%*s%s", left, "", s) — left=5 for width=10, empty s
	if !strings.HasPrefix(got, " ") {
		t.Errorf("centerPad('', 10): expected leading spaces, got %q", got)
	}
}
