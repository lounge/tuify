package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The frame must be exactly the terminal height. One line more and
// bubbletea drops the top line, and every mouse zone in the list is then
// one row off from the row the user sees. The search prompt used to add a
// sixth line to the now-playing bar.
func TestView_SearchPromptKeepsFrameAtTerminalHeight(t *testing.T) {
	const height = 24
	m := newIntentTestModel()
	tv := newTrackView(m.rootCtx, m.client, "p1", "Roadtrip", 0, 0, false)
	m.pushView(tv)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	m = updated.(Model)

	frameLines := func() int { return strings.Count(m.View(), "\n") + 1 }
	if got := frameLines(); got != height {
		t.Fatalf("setup: frame is %d lines, want %d", got, height)
	}

	m, _ = pressKeys(t, m, runeKey("/"))
	if !tv.searching {
		t.Fatal("/ did not open the filter")
	}
	if got := frameLines(); got != height {
		t.Errorf("frame is %d lines with the search prompt open, want %d", got, height)
	}

	// A status banner replaces the track line; the prompt must not grow
	// the bar there either.
	m.nowPlaying.statusMsg = "Copied link to clipboard"
	if got := frameLines(); got != height {
		t.Errorf("frame is %d lines with a status banner and the prompt, want %d", got, height)
	}
}

func newTestModel(width int, np *nowPlayingModel) Model {
	np.width = width
	return Model{
		width:      width,
		height:     24,
		nowPlaying: np,
		miniMode:   true,
	}
}

func TestMiniModeView_NoTrack(t *testing.T) {
	np := &nowPlayingModel{hasTrack: false}
	m := newTestModel(80, np)
	result := m.miniModeView()
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(result, "No track playing") {
		t.Errorf("expected 'No track playing', got %q", result)
	}
}

func TestMiniModeView_Playing(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:   true,
		playing:    true,
		track:      "Test Song",
		artist:     "Test Artist",
		progressMs: 60000,
		durationMs: 200000,
	}
	m := newTestModel(80, np)
	result := m.miniModeView()

	if !strings.Contains(result, "Test Song") {
		t.Error("expected track name in output")
	}
	if !strings.Contains(result, "Test Artist") {
		t.Error("expected artist name in output")
	}
	if !strings.Contains(result, "1:00") {
		t.Error("expected current time in output")
	}
}

func TestMiniModeView_Paused(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack: true,
		playing:  false,
		track:    "Song",
		artist:   "Artist",
	}
	m := newTestModel(80, np)
	result := m.miniModeView()
	if !strings.Contains(result, "⏸") {
		t.Error("expected pause icon")
	}
}

func TestMiniModeView_Shuffle(t *testing.T) {
	tests := []struct {
		name     string
		nerdFont bool
		want     string
	}{
		{name: "fallback", nerdFont: false, want: shuffleIconFallback},
		{name: "nerd font", nerdFont: true, want: shuffleIconNerdFont},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := &nowPlayingModel{
				hasTrack:   true,
				playing:    true,
				shuffling:  true,
				nerdFont:   tt.nerdFont,
				track:      "This Is A Very Long Track Name",
				artist:     "This Is A Very Long Artist Name",
				durationMs: 60000,
			}
			m := newTestModel(50, np)
			result := m.miniModeView()
			if !strings.Contains(result, tt.want) {
				t.Errorf("expected shuffle icon %q in %q", tt.want, result)
			}
			if w := lipgloss.Width(result); w > 50 {
				t.Errorf("width %d exceeds terminal width 50", w)
			}

			np.shuffling = false
			if strings.Contains(m.miniModeView(), tt.want) {
				t.Errorf("shuffle icon %q shown while shuffle is off", tt.want)
			}
		})
	}
}

func TestMiniModeView_StatusMessage(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:  true,
		statusMsg: "Something went wrong",
	}
	m := newTestModel(80, np)
	result := m.miniModeView()
	if !strings.Contains(result, "Something went wrong") {
		t.Error("expected status message in output")
	}
}

func TestMiniModeView_NarrowTerminal(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:   true,
		playing:    true,
		track:      "A Very Long Track Name That Should Be Truncated",
		artist:     "An Artist With A Long Name",
		progressMs: 30000,
		durationMs: 180000,
	}
	m := newTestModel(40, np)
	result := m.miniModeView()
	width := lipgloss.Width(result)
	if width > 40 {
		t.Errorf("output width %d exceeds terminal width 40", width)
	}
}

func TestMiniModeView_VeryNarrowTerminal(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:   true,
		playing:    true,
		track:      "Song",
		artist:     "Artist",
		progressMs: 0,
		durationMs: 60000,
	}
	m := newTestModel(20, np)
	// Should not panic.
	result := m.miniModeView()
	if result == "" {
		t.Error("expected non-empty output even at narrow width")
	}
}

// When the label doesn't fit, mini mode marquee-scrolls it rather than
// truncating with "…". Pin that the output stays single-line and fits the
// terminal width, since a wrapped mini line would break zone coordinates
// and push the UI off-screen.
func TestMiniModeView_LongLabelFitsAndScrolls(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:   true,
		playing:    true,
		track:      "This Is A Very Long Track Name",
		artist:     "This Is A Very Long Artist Name",
		progressMs: 0,
		durationMs: 60000,
	}
	m := newTestModel(50, np)
	first := m.miniModeView()
	if strings.Contains(first, "\n") {
		t.Fatalf("miniModeView wrapped: %q", first)
	}

	// Advance the marquee and render again — the visible window should
	// shift, proving the label is scrolling rather than statically truncated.
	np.labelScrollOffset = 5
	second := m.miniModeView()
	if second == first {
		t.Error("miniModeView output unchanged after advancing labelScrollOffset; marquee not active")
	}
}
