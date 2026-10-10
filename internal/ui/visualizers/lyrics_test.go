package visualizers

import (
	"strings"
	"testing"
)

// untimed builds untimed lyric lines, as Genius or plain LRCLIB text gives.
func untimed(texts ...string) []LyricLine {
	lines := make([]LyricLine, len(texts))
	for i, t := range texts {
		lines[i] = LyricLine{StartMs: -1, Text: t}
	}
	return lines
}

func TestLyrics_ViewBeforeInit(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	got := l.View(80, 10)
	if got != "" {
		t.Errorf("View before Init should return empty, got %q", got)
	}
}

func TestLyrics_ViewZeroDimensions(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)

	got := l.View(80, 10)
	if !strings.Contains(got, "Loading lyrics") {
		t.Errorf("expected loading message, got %q", got)
	}
}

func TestLyrics_NoLyrics(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(nil)

	got := l.View(80, 10)
	if !strings.Contains(got, "No lyrics found") {
		t.Errorf("expected no lyrics message, got %q", got)
	}
}

func TestLyrics_Instrumental(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetInstrumental()

	got := l.View(80, 10)
	if !strings.Contains(got, "Instrumental") {
		t.Errorf("expected instrumental message, got %q", got)
	}
}

func TestLyrics_SetLyricsClearsLoading(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(untimed("Line one", "Line two"))

	got := l.View(80, 10)
	if strings.Contains(got, "Loading") {
		t.Error("should not show loading after SetLyrics")
	}
}

func TestLyrics_SetInstrumentalClearsLoading(t *testing.T) {
	t.Parallel()

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

func TestLyrics_SetLyricsDetectsSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []LyricLine
		want  bool
	}{
		{"all timed", []LyricLine{{StartMs: 0, Text: "a"}, {StartMs: 5000, Text: "b"}}, true},
		{"all untimed", untimed("a", "b"), false},
		{"one untimed line disables sync", []LyricLine{{StartMs: 0, Text: "a"}, {StartMs: -1, Text: "b"}}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := NewLyrics()
			l.Init("seed", 10000)
			l.SetLyrics(tt.lines)
			if l.synced != tt.want {
				t.Errorf("synced = %v, want %v", l.synced, tt.want)
			}
		})
	}
}

func TestLyrics_ViewDimensions(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	lines := make([]LyricLine, 30)
	for i := range lines {
		lines[i] = LyricLine{StartMs: -1, Text: "Lyric line"}
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
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)

	lines := make([]LyricLine, 20)
	for i := range lines {
		lines[i] = LyricLine{StartMs: -1, Text: "Line " + string(rune('A'+i))}
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

// A negative position (stale or interpolated progress) must clamp to the
// first line, not index the lines slice with a negative number.
func TestLyrics_NegativeProgressClampsToStart(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(untimed("first", "second", "third"))

	l.SetProgress(0)
	atStart := l.View(40, 3)
	l.SetProgress(-5000)
	if got := l.View(40, 3); got != atStart {
		t.Errorf("negative progress rendered differently from progress 0:\n%q\nvs\n%q", got, atStart)
	}
}

// Timed lines: the current line is the last one whose stamp has been
// reached, looking lyricsLeadMs ahead of the reported position.
func TestLyrics_CurrentLineSynced(t *testing.T) {
	t.Parallel()

	lines := []LyricLine{
		{StartMs: 1000, Text: "first"},
		{StartMs: 5000, Text: "second"},
		{StartMs: 9000, Text: "third"},
		{StartMs: 12000, Text: ""}, // instrumental break
	}
	tests := []struct {
		name       string
		progressMs int
		want       int
	}{
		{"before the first stamp", 0, -1},
		{"lead not yet reaching the first stamp", 1000 - lyricsLeadMs - 1, -1},
		{"lead reaches the first stamp", 1000 - lyricsLeadMs, 0},
		{"exactly on a stamp", 5000, 1},
		{"between stamps", 7000, 1},
		{"just before the next stamp minus lead", 9000 - lyricsLeadMs - 1, 1},
		{"stamped empty line is current", 12500, 3},
		{"past the last stamp stays on the last line", 999999, 3},
		{"negative progress", -5000, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := NewLyrics()
			l.Init("seed", 200000)
			l.SetLyrics(lines)
			l.SetProgress(tt.progressMs)
			if got := l.currentLine(); got != tt.want {
				t.Errorf("currentLine() at %d ms = %d, want %d", tt.progressMs, got, tt.want)
			}
		})
	}
}

// Timed lines ignore the track duration: with no stamp reached yet,
// nothing is current even though the proportional estimate would pick a
// line.
func TestLyrics_SyncedIgnoresProportionalEstimate(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics([]LyricLine{{StartMs: 8000, Text: "late first"}, {StartMs: 9000, Text: "second"}})
	l.SetProgress(5000) // halfway: proportional would pick a line
	if got := l.currentLine(); got != -1 {
		t.Errorf("currentLine() = %d, want -1 before the first stamp", got)
	}
}

// Before the first timed line the window is pinned to the top, so the
// opening lines are visible while the intro plays.
func TestLyrics_SyncedViewPinsTopBeforeFirstLine(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 200000)
	lines := make([]LyricLine, 20)
	for i := range lines {
		lines[i] = LyricLine{StartMs: 30000 + i*5000, Text: "line " + string(rune('a'+i))}
	}
	l.SetLyrics(lines)
	l.SetProgress(0)

	got := l.View(40, 3)
	for _, want := range []string{"line a", "line b", "line c"} {
		if !strings.Contains(got, want) {
			t.Errorf("view before the first stamp lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "line d") {
		t.Errorf("view before the first stamp is not pinned to the top:\n%s", got)
	}
}

// The window follows the current timed line.
func TestLyrics_SyncedViewFollowsCurrentLine(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 200000)
	lines := make([]LyricLine, 20)
	for i := range lines {
		lines[i] = LyricLine{StartMs: i * 5000, Text: "line " + string(rune('a'+i))}
	}
	l.SetLyrics(lines)
	l.SetProgress(50000) // line k (index 10)

	got := l.View(40, 3)
	if !strings.Contains(got, "line k") {
		t.Errorf("view at 50 s does not show the current line:\n%s", got)
	}
	if strings.Contains(got, "line a") {
		t.Errorf("view at 50 s still shows the first line:\n%s", got)
	}
}

// Untimed lines: progress maps onto the content lines only, so section
// headers and blank lines do not pull the estimate off.
func TestLyrics_UnsyncedSkipsSectionsAndBlanks(t *testing.T) {
	t.Parallel()

	lines := untimed("[Verse 1]", "a", "b", "", "[Chorus]", "c", "d")
	tests := []struct {
		name       string
		progressMs int
		want       int
	}{
		{"start lands on the first content line", 0, 1},
		{"halfway lands on the third content line", 5000, 5},
		{"end lands on the last content line", 9999, 6},
		{"past the end clamps to the last content line", 20000, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := NewLyrics()
			l.Init("seed", 10000)
			l.SetLyrics(lines)
			l.SetProgress(tt.progressMs)
			if got := l.currentLine(); got != tt.want {
				t.Errorf("currentLine() at %d ms = %d, want %d", tt.progressMs, got, tt.want)
			}
		})
	}
}

// Lyrics made only of headers and blanks still render and never index
// out of range.
func TestLyrics_UnsyncedAllSectionsFallsBackToRawIndex(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(untimed("[Intro]", "", "[Outro]"))

	for _, p := range []int{0, 5000, 10000, 20000} {
		l.SetProgress(p)
		if got := l.currentLine(); got < 0 || got > 2 {
			t.Errorf("currentLine() at %d ms = %d, out of range", p, got)
		}
		if l.View(20, 3) == "" {
			t.Errorf("View at %d ms returned nothing", p)
		}
	}
}

func TestLyrics_AdvanceIsNoop(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics(untimed("test"))

	// Advance should not panic or change state
	l.Advance()
}

func TestLyrics_ReinitResetsState(t *testing.T) {
	t.Parallel()

	l := NewLyrics()
	l.Init("seed", 10000)
	l.SetLyrics([]LyricLine{{StartMs: 0, Text: "old lyrics"}})

	// Re-init should reset to loading
	l.Init("new-seed", 20000)
	if !l.loading {
		t.Error("should be loading after re-init")
	}
	if l.lines != nil {
		t.Error("lines should be nil after re-init")
	}
	if l.synced {
		t.Error("synced should be false after re-init")
	}
	if l.contentIdx != nil {
		t.Error("contentIdx should be nil after re-init")
	}
}

func TestLyricGray_Dark(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	// Light mode: grays should increase with distance (darker = lower number)
	g0 := lyricGray(false, false, 0)
	g5 := lyricGray(false, false, 5)
	if g5 <= g0 {
		t.Errorf("light gray should increase with distance: dist=0 %d, dist=5 %d", g0, g5)
	}
}

func TestCenterPad(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	got := centerPad("", 10)
	// centerPad uses fmt.Sprintf("%*s%s", left, "", s) — left=5 for width=10, empty s
	if !strings.HasPrefix(got, " ") {
		t.Errorf("centerPad('', 10): expected leading spaces, got %q", got)
	}
}
