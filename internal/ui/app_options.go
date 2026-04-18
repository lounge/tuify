package ui

import "github.com/lounge/tuify/internal/audio"

// ModelOption configures optional Model features.
type ModelOption func(*Model)

// AudioSource provides real-time FFT data for the visualizer.
// Implemented by audio.PipeReader.
type AudioSource interface {
	Latest() *audio.FrequencyData
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
