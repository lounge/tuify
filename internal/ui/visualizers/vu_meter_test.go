package visualizers

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/audio"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func TestVUMeter_ViewBeforeInit(t *testing.T) {
	v := NewVUMeter()
	if got := v.View(100, 14); got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestVUMeter_AdvanceBeforeInit(t *testing.T) {
	NewVUMeter().Advance() // must not panic
}

func TestVUMeter_ViewZeroDimensions(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	if got := v.View(0, 10); got != "" {
		t.Errorf("width=0 should return empty, got %q", got)
	}
	if got := v.View(10, 0); got != "" {
		t.Errorf("height=0 should return empty, got %q", got)
	}
}

func TestVUMeter_ViewDimensions(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	for _, sz := range []struct{ w, h int }{
		{40, vuMinDialHeight}, // narrow + short → bar fallback
		{30, 18},              // narrow + tall → stacked
		{100, 14},             // wide → side-by-side
		{120, 18},
	} {
		got := v.View(sz.w, sz.h)
		lines := strings.Split(got, "\n")
		if len(lines) != sz.h {
			t.Errorf("size %dx%d: expected %d lines, got %d", sz.w, sz.h, sz.h, len(lines))
		}
	}
}

func TestVUMeter_NoPanicAtTinySizes(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	for _, sz := range []struct{ w, h int }{
		{1, 1},
		{2, 2},
		{8, 6},
		{20, 4},
	} {
		_ = v.View(sz.w, sz.h) // must not panic
	}
}

func TestVUMeter_StackedLayoutWhenNarrow(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1.0, RightLevel: 0.5})
	for i := 0; i < 200; i++ {
		v.Advance()
	}
	// Width too narrow for side-by-side (each would be 14 < 22) but tall
	// enough for two stacked dials at vuMinDialHeight each.
	got := v.View(30, 18)
	lines := strings.Split(got, "\n")
	if len(lines) != 18 {
		t.Fatalf("expected 18 lines, got %d", len(lines))
	}
	plain := stripANSI(got)
	if !strings.Contains(plain, "LEFT") || !strings.Contains(plain, "RIGHT") {
		t.Errorf("stacked layout should render LEFT and RIGHT channel labels; got:\n%s", plain)
	}
	// Channel labels should appear top-to-bottom in LEFT, RIGHT order.
	leftIdx := strings.Index(plain, "LEFT")
	rightIdx := strings.Index(plain, "RIGHT")
	if leftIdx < 0 || rightIdx < 0 || leftIdx > rightIdx {
		t.Errorf("LEFT should appear above RIGHT in stacked layout; got:\n%s", plain)
	}
}

func TestVUMeter_BarFallbackWhenTiny(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1.0, RightLevel: 0.5})
	for i := 0; i < 200; i++ {
		v.Advance()
	}
	// Too small for a single dial in either orientation: bar fallback.
	got := v.View(40, 6)
	plain := stripANSI(got)
	if !strings.Contains(plain, "LEFT ") {
		t.Errorf("fallback should label the LEFT bar; got:\n%s", plain)
	}
	if !strings.Contains(plain, "RIGHT ") {
		t.Errorf("fallback should label the RIGHT bar; got:\n%s", plain)
	}
}

func TestVUMeter_InitResetsBothChannels(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1.0, RightLevel: 1.0})
	for i := 0; i < 200; i++ {
		v.Advance()
	}
	if v.leftDb <= vuDbMin+1 || v.rightDb <= vuDbMin+1 {
		t.Fatalf("needles should have climbed; got L=%.2f R=%.2f", v.leftDb, v.rightDb)
	}
	v.Init("seed2", 10000)
	if v.leftDb != vuDbMin || v.rightDb != vuDbMin {
		t.Errorf("Init should reset both needles to %v; got L=%.2f R=%.2f", vuDbMin, v.leftDb, v.rightDb)
	}
}

func TestVUMeter_StereoBalanceIsIndependent(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	// Loud left, quiet right.
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1.0, RightLevel: 0.05})
	for i := 0; i < 300; i++ {
		v.Advance()
	}
	if v.leftDb-v.rightDb < 5 {
		t.Errorf("L should sit much higher than R; got L=%.2f R=%.2f", v.leftDb, v.rightDb)
	}
}

func TestVUMeter_NilAudioDecaysBothToFloor(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1.0, RightLevel: 1.0})
	for i := 0; i < 200; i++ {
		v.Advance()
	}
	v.SetAudioData(nil)
	for i := 0; i < 500; i++ {
		v.Advance()
	}
	if math.Abs(v.leftDb-vuDbMin) > 0.01 || math.Abs(v.rightDb-vuDbMin) > 0.01 {
		t.Errorf("nil audio should decay both to %.1f dB; got L=%.3f R=%.3f", vuDbMin, v.leftDb, v.rightDb)
	}
}

func TestVUMeter_LevelClampedToMaxDb(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	// LeftLevel=1.0 → 0 dB which is below vuDbMax (+3). To probe the
	// upper clamp, exploit clampUnit-style upstream behavior: feed a
	// level that would correspond to > +3 dB if unclamped.
	v.SetAudioData(&audio.FrequencyData{
		LeftLevel:  float32(math.Pow(10, (vuDbMax+5)/20)),
		RightLevel: 1.0,
	})
	for i := 0; i < 500; i++ {
		v.Advance()
	}
	if v.leftDb > vuDbMax+0.01 {
		t.Errorf("L needle should be clamped to %.1f dB, got %.3f", vuDbMax, v.leftDb)
	}
}

func TestVUMeter_QuietLevelClampedToMinDb(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	v.SetAudioData(&audio.FrequencyData{LeftLevel: 1e-8, RightLevel: 1e-8})
	for i := 0; i < 500; i++ {
		v.Advance()
	}
	if v.leftDb < vuDbMin-0.01 {
		t.Errorf("L should not fall below %.1f, got %.3f", vuDbMin, v.leftDb)
	}
}

func TestVUMeter_RenderShowsChannels(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	plain := stripANSI(v.View(100, 14))
	if !strings.Contains(plain, "LEFT") || !strings.Contains(plain, "RIGHT") {
		t.Errorf("dial render should include both channel labels; got:\n%s", plain)
	}
}

func TestVUMeter_RenderShowsScaleExtremes(t *testing.T) {
	v := NewVUMeter()
	v.Init("seed", 10000)
	plain := stripANSI(v.View(100, 14))
	if !strings.Contains(plain, "-20") {
		t.Errorf("dial render should include the -20 dB label; got:\n%s", plain)
	}
	if !strings.Contains(plain, "+3") {
		t.Errorf("dial render should include the +3 dB label; got:\n%s", plain)
	}
}

func TestVUMeter_Deterministic(t *testing.T) {
	a := NewVUMeter()
	b := NewVUMeter()
	a.Init("seed", 10000)
	b.Init("seed", 10000)
	for i := 0; i < 50; i++ {
		a.SetAudioData(&audio.FrequencyData{LeftLevel: 0.3, RightLevel: 0.6})
		b.SetAudioData(&audio.FrequencyData{LeftLevel: 0.3, RightLevel: 0.6})
		a.Advance()
		b.Advance()
	}
	if a.View(100, 14) != b.View(100, 14) {
		t.Error("identical input streams should produce identical renders")
	}
}
