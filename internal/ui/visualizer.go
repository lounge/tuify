package ui

import (
	"context"
	"image"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
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
	audioSeenAt time.Time // sticky flag for "audio was flowing recently"
}

// audioStickyWindow keeps audioFlowing() true for this long after the
// last non-nil Latest(), so cycle decisions stay stable across the
// 150ms FFT-staleness threshold and through brief frame-arrival jitter.
const audioStickyWindow = 3 * time.Second

func newVisualizerModel(hasAudio bool) *visualizerModel {
	var vizList []visualizers.Visualizer
	if hasAudio {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
			visualizers.NewStarfield(),
			visualizers.NewSpectrum(),
			visualizers.NewOscillogram(),
			visualizers.NewVUMeter(),
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
	m.refreshAudioSeen()
	if trackID != m.trackID {
		m.initTrack(trackID, durationMs, track, artist, isEpisode)
	}
	if m.shouldSkip(m.vizIdx) {
		m.cycle(1)
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
	data := m.refreshAudioSeen()
	v := m.viz()
	if aa, ok := v.(visualizers.AudioAware); ok {
		aa.SetAudioData(data)
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
	if n == 0 {
		return
	}
	orig := m.vizIdx
	for i := 0; i < n; i++ {
		m.vizIdx = (m.vizIdx + delta + n) % n
		if !m.shouldSkip(m.vizIdx) {
			return
		}
	}
	// AlbumArt is always non-skippable in the current list, so the loop
	// always finds a slot in practice. This restore is defensive against
	// future list changes that could leave the user stranded on the
	// last (skipped) slot the loop visited.
	m.vizIdx = orig
}

// shouldSkip returns true if the visualizer at idx is meaningless in the
// current playback state — episode + lyrics, or audio-reactive while no
// fresh PCM has been seen for audioStickyWindow.
func (m *visualizerModel) shouldSkip(idx int) bool {
	if m.isEpisode && m.isLyricsViz(idx) {
		return true
	}
	if _, audioAware := m.vizList[idx].(visualizers.AudioAware); audioAware {
		if !m.audioFlowing() {
			return true
		}
	}
	return false
}

// audioFlowing reports whether audio was seen flowing within the sticky
// window. Read-only; refreshAudioSeen is the writer.
func (m *visualizerModel) audioFlowing() bool {
	if m.audioSrc == nil || m.audioSeenAt.IsZero() {
		return false
	}
	return time.Since(m.audioSeenAt) < audioStickyWindow
}

// refreshAudioSeen probes the audio source, bumps the sticky timestamp
// if a fresh frame is available, and returns the frame (or nil). Called
// from advance() each tick and from toggle() when the pane opens, so
// shouldSkip's view of the world stays current without polling on the
// cycle hot path.
func (m *visualizerModel) refreshAudioSeen() *audio.FrequencyData {
	if m.audioSrc == nil {
		return nil
	}
	data := m.audioSrc.Latest()
	if data != nil {
		m.audioSeenAt = time.Now()
	}
	return data
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
	if m.shouldSkip(m.vizIdx) {
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
