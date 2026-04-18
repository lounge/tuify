package ui

import (
	"context"
	"image"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type vizTickMsg struct{}

type visualizerModel struct {
	ctx         context.Context // app-level ctx; wrapped with per-op timeout in fetch helpers
	active      bool
	trackID     string
	isEpisode   bool
	vizList     []visualizers.Visualizer
	vizIdx      int
	imageURL    string
	images      asyncLoader[fetchResult]
	imageCache  boundedCache[string, image.Image]
	lyrics      asyncLoader[lyricsFetchResult]
	lyricsCache boundedCache[string, cachedLyrics]
	audioSrc    AudioSource
}

func newVisualizerModel(hasAudio bool) *visualizerModel {
	var vizList []visualizers.Visualizer
	if hasAudio {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
			visualizers.NewStarfield(),
			visualizers.NewSpectrum(),
			visualizers.NewOscillogram(),
			visualizers.NewSpectrogram(),
			visualizers.NewMilkdropSpiral(),
			visualizers.NewMilkdropTunnel(),
			visualizers.NewMilkdropKaleidoscope(),
			visualizers.NewMilkdropRipple(),
		}
	} else {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
		}
	}
	// ctx is left zero; NewModel sets it from Model.rootCtx after options
	// apply. Any code path that triggers loadImage or loadLyrics must go
	// through NewModel — direct construction is for tests that don't
	// exercise those paths.
	return &visualizerModel{
		vizList:     vizList,
		images:      newAsyncLoader[fetchResult](),
		imageCache:  newBoundedCache[string, image.Image](20),
		lyrics:      newAsyncLoader[lyricsFetchResult](),
		lyricsCache: newBoundedCache[string, cachedLyrics](20),
	}
}

func (m *visualizerModel) viz() visualizers.Visualizer {
	return m.vizList[m.vizIdx]
}

func (m *visualizerModel) toggle(trackID string, durationMs int, imageURL, track, artist string, isEpisode bool) tea.Cmd {
	if m.active {
		m.active = false
		return nil
	}
	m.active = true
	m.drainImages()
	m.drainLyrics()
	if trackID != m.trackID {
		m.initTrack(trackID, durationMs, track, artist, isEpisode)
	}
	m.loadImage(imageURL)
	return m.tick()
}

func (m *visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance(progressMs int) {
	m.drainImages()
	m.drainLyrics()
	v := m.viz()
	if m.audioSrc != nil {
		if aa, ok := v.(visualizers.AudioAware); ok {
			aa.SetAudioData(m.audioSrc.Latest())
		}
	}
	if pa, ok := v.(visualizers.ProgressAware); ok {
		pa.SetProgress(progressMs)
	}
	v.Advance()
}

func (m *visualizerModel) isLyricsViz(idx int) bool {
	_, ok := m.vizList[idx].(*visualizers.Lyrics)
	return ok
}

func (m *visualizerModel) cycle(delta int) {
	n := len(m.vizList)
	m.vizIdx = (m.vizIdx + delta + n) % n
	if m.isEpisode && m.isLyricsViz(m.vizIdx) {
		m.vizIdx = (m.vizIdx + delta + n) % n
	}
}

func (m *visualizerModel) onTrackChange(trackID string, durationMs int, track, artist string, isEpisode bool) {
	m.initTrack(trackID, durationMs, track, artist, isEpisode)
}

func (m *visualizerModel) initTrack(trackID string, durationMs int, track, artist string, isEpisode bool) {
	m.trackID = trackID
	m.isEpisode = isEpisode
	for _, v := range m.vizList {
		v.Init(trackID, durationMs)
	}
	if !isEpisode {
		m.loadLyrics(trackID, track, artist)
	}
	if isEpisode && m.isLyricsViz(m.vizIdx) {
		m.cycle(1)
	}
}

func (m *visualizerModel) View(width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	return m.viz().View(width, height)
}
