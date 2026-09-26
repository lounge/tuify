package ui

import (
	"fmt"
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

// BenchmarkVisualizerFrame renders whole frames (visualizer plus the
// now-playing bar, through Model.View) at 120x40 and a large 250x70
// terminal. It covers what the per-visualizer benchmarks don't: the frame
// assembly and whether zone.Scan runs over it.
func BenchmarkVisualizerFrame(b *testing.B) {
	for _, sz := range []struct{ w, h int }{{120, 40}, {250, 70}} {
		b.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(b *testing.B) {
			m := newIntentTestModel()
			m.nowPlaying = benchNowPlaying()
			m.visualizer = newVisualizerModel(m.rootCtx, &fakeAudioSource{})
			m.visualizer.active = true
			m.visualizer.trackID = "abc"
			updated, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			m = updated.(Model)
			b.ReportAllocs()
			for b.Loop() {
				m.View()
			}
		})
	}
}
