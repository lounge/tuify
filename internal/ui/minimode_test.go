package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

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

func TestMiniModeView_Truncation(t *testing.T) {
	np := &nowPlayingModel{
		hasTrack:   true,
		playing:    true,
		track:      "This Is A Very Long Track Name",
		artist:     "This Is A Very Long Artist Name",
		progressMs: 0,
		durationMs: 60000,
	}
	m := newTestModel(50, np)
	result := m.miniModeView()
	if !strings.Contains(result, "…") {
		t.Error("expected truncation ellipsis in narrow output")
	}
}
