package ui

import (
	"testing"
	"time"

	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

// fakeAudioSource is a controllable AudioSource for tests. Latest()
// returns whatever data is stored on `frame`.
type fakeAudioSource struct {
	frame *audio.FrequencyData
}

func (f *fakeAudioSource) Latest() *audio.FrequencyData { return f.frame }

// newTestVizModel builds a visualizerModel with the full audio-reactive
// vizList and the given audio source already attached. The list layout
// is the one newVisualizerModel(true) ships:
//
//	0  AlbumArt   (not AudioAware)
//	1  Lyrics     (not AudioAware)
//	2  Starfield  (AudioAware)
//	3+ Spectrum, Oscillogram, Spectrogram, VUMeter, Milkdrop* (all AudioAware)
func newTestVizModel(src AudioSource) *visualizerModel {
	m := newVisualizerModel(true)
	m.audioSrc = src
	return m
}

func TestVisualizer_CycleSkipsAudioVizWhenNoFlow(t *testing.T) {
	src := &fakeAudioSource{} // Latest() returns nil
	m := newTestVizModel(src)
	// Start on AlbumArt (idx 0); cycle forward — Lyrics (idx 1) is non-audio
	// so we should land there.
	m.cycle(1)
	if _, isAudio := m.vizList[m.vizIdx].(visualizers.AudioAware); isAudio {
		t.Errorf("cycle should have landed on a non-audio viz; got idx %d which is AudioAware", m.vizIdx)
	}
	if m.vizIdx != 1 {
		t.Errorf("expected idx 1 (Lyrics), got %d", m.vizIdx)
	}

	// Cycle forward again — every remaining slot is audio-aware, so the
	// loop wraps and lands on AlbumArt (idx 0).
	m.cycle(1)
	if m.vizIdx != 0 {
		t.Errorf("expected wrap to idx 0 (AlbumArt), got %d", m.vizIdx)
	}
}

func TestVisualizer_CycleIncludesAudioVizWhenFlowing(t *testing.T) {
	src := &fakeAudioSource{frame: &audio.FrequencyData{}}
	m := newTestVizModel(src)
	// Prime the sticky flag (simulates a tick having already arrived).
	m.refreshAudioSeen()

	m.cycle(1) // → Lyrics
	m.cycle(1) // → Starfield (first audio-aware)
	if m.vizIdx != 2 {
		t.Errorf("expected idx 2 (Starfield), got %d", m.vizIdx)
	}
	if _, isAudio := m.vizList[m.vizIdx].(visualizers.AudioAware); !isAudio {
		t.Errorf("expected an AudioAware viz; idx %d is not", m.vizIdx)
	}
}

func TestVisualizer_StickyWindowKeepsCycleStableThroughGaps(t *testing.T) {
	src := &fakeAudioSource{frame: &audio.FrequencyData{}}
	m := newTestVizModel(src)
	m.refreshAudioSeen()

	// Audio momentarily disappears (FFT frame-arrival jitter); within the
	// sticky window the cycle should still treat audio as flowing.
	src.frame = nil
	if !m.audioFlowing() {
		t.Error("audioFlowing should remain true within sticky window after a transient nil Latest()")
	}

	// Expire the sticky window by hand-rewinding the timestamp.
	m.audioSeenAt = time.Now().Add(-audioStickyWindow - time.Second)
	if m.audioFlowing() {
		t.Error("audioFlowing should be false once sticky window has expired")
	}
}

func TestVisualizer_AudioFlowingFalseBeforeFirstFrame(t *testing.T) {
	m := newTestVizModel(&fakeAudioSource{})
	if m.audioFlowing() {
		t.Error("audioFlowing should be false before any frame was observed")
	}
}

func TestVisualizer_AudioFlowingFalseWithoutSource(t *testing.T) {
	m := newVisualizerModel(true)
	// no audioSrc attached
	m.audioSeenAt = time.Now() // even with a stale timestamp it should be false
	if m.audioFlowing() {
		t.Error("audioFlowing should be false when audioSrc is nil regardless of timestamp")
	}
}

func TestVisualizer_RefreshAudioSeenReturnsFrame(t *testing.T) {
	frame := &audio.FrequencyData{}
	src := &fakeAudioSource{frame: frame}
	m := newTestVizModel(src)
	if got := m.refreshAudioSeen(); got != frame {
		t.Errorf("refreshAudioSeen should return the underlying frame; got %v want %v", got, frame)
	}
	if m.audioSeenAt.IsZero() {
		t.Error("audioSeenAt should be set after a non-nil refresh")
	}
}

func TestVisualizer_RefreshAudioSeenDoesNotBumpOnNil(t *testing.T) {
	m := newTestVizModel(&fakeAudioSource{})
	if got := m.refreshAudioSeen(); got != nil {
		t.Errorf("refreshAudioSeen should return nil when Latest is nil; got %v", got)
	}
	if !m.audioSeenAt.IsZero() {
		t.Error("audioSeenAt should remain zero when no frame is observed")
	}
}

func TestVisualizer_ShouldSkipEpisodeLyrics(t *testing.T) {
	src := &fakeAudioSource{frame: &audio.FrequencyData{}}
	m := newTestVizModel(src)
	m.refreshAudioSeen()
	m.isEpisode = true
	// Lyrics is at idx 1.
	if !m.shouldSkip(1) {
		t.Error("shouldSkip should return true for Lyrics during an episode")
	}
	// AlbumArt (idx 0) is fine even for episodes.
	if m.shouldSkip(0) {
		t.Error("shouldSkip should not skip AlbumArt during an episode")
	}
}

func TestVisualizer_CycleNeverInfiniteLoops(t *testing.T) {
	// Construct a model with no audio source and force isEpisode so Lyrics
	// is also skipped. Even with most viz unreachable, cycle must return.
	m := newVisualizerModel(true)
	m.isEpisode = true
	// audioSrc nil → all AudioAware viz are skipped. Only AlbumArt remains
	// reachable in the cycle.
	m.cycle(1)
	if m.vizIdx != 0 {
		t.Errorf("expected to settle on AlbumArt (idx 0); got %d", m.vizIdx)
	}
	m.cycle(1)
	if m.vizIdx != 0 {
		t.Errorf("cycle from AlbumArt with only AlbumArt reachable should stay at 0; got %d", m.vizIdx)
	}
}
