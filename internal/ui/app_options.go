package ui

import (
	"context"

	"github.com/lounge/tuify/internal/audio"
)

// ModelOption configures optional Model features.
type ModelOption func(*Model)

// AudioSource provides real-time FFT data for the visualizer.
// Implemented by audio.PipeReader.
type AudioSource interface {
	Latest() *audio.FrequencyData
}

// volumeConsumer is an optional AudioSource capability. When the source
// implements it, the UI pushes the current Spotify device volume so the
// source can compensate FFT output for softvol-scaled PCM.
type volumeConsumer interface {
	SetVolumePercent(int)
}

// WithAudioSource sets the audio source for real-time visualizer data
// and enables the audio-reactive visualizers.
func WithAudioSource(src AudioSource) ModelOption {
	return func(m *Model) {
		if src != nil {
			m.visualizer = newVisualizerModel(true)
			m.visualizer.audioSrc = src
		}
	}
}

// WithVimMode enables vim-style keybindings (h/l for back/select, ctrl+d/u half-page, etc.).
func WithVimMode() ModelOption {
	return func(m *Model) { m.vimMode = true }
}

// WithRootContext sets the app-level context. All Spotify API calls
// spawned by the UI wrap this with per-operation timeouts so pending
// requests cancel when the context is cancelled (e.g. on app shutdown).
// Panics on nil ctx — forgetting to plumb the root ctx would silently
// downgrade shutdown semantics; failing loudly surfaces the bug at
// startup instead of masking it.
func WithRootContext(ctx context.Context) ModelOption {
	if ctx == nil {
		panic("ui.WithRootContext: ctx must not be nil")
	}
	return func(m *Model) { m.rootCtx = ctx }
}

// WithLibrespotInactive provides a channel that signals when librespot reports
// its device became inactive (playback moved to another device).
func WithLibrespotInactive(ch <-chan struct{}) ModelOption {
	return func(m *Model) { m.librespotInactiveCh = ch }
}

// WithTokenSaveErrors provides a channel that emits OAuth token persistence
// failures. Each value is rendered as a visible warning so the user can tell
// why they're getting logged out between sessions.
func WithTokenSaveErrors(ch <-chan error) ModelOption {
	return func(m *Model) { m.tokenSaveErrCh = ch }
}
