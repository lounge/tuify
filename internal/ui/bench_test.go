package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func benchNowPlaying() *nowPlayingModel {
	return &nowPlayingModel{
		width:      120,
		hasTrack:   true,
		playing:    true,
		track:      "A Reasonably Long Track Title",
		artist:     "Some Artist",
		trackURI:   "spotify:track:abc",
		progressMs: 60000,
		durationMs: 200000,
	}
}

// BenchmarkRenderNowPlaying covers the now-playing bar, redrawn on every
// progress tick, and its gradient pass on its own.
func BenchmarkRenderNowPlaying(b *testing.B) {
	np := benchNowPlaying()
	b.Run("View", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			np.View(false, "")
		}
	})
	b.Run("renderGradient", func(b *testing.B) {
		lines := []string{"", np.renderTrackLine(), "", np.progressBarView(), ""}
		b.ReportAllocs()
		for b.Loop() {
			np.renderGradient(lines)
		}
	})
}

// BenchmarkHomeView renders a full frame of the home screen at 120x40.
func BenchmarkHomeView(b *testing.B) {
	m := newIntentTestModel()
	m.nowPlaying = benchNowPlaying()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	b.ReportAllocs()
	for b.Loop() {
		m.View()
	}
}
