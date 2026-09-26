package ui

import "github.com/lounge/tuify/internal/audio"

// ModelOption configures optional Model features.
type ModelOption func(*modelOptions)

// modelOptions collects the options before NewModel builds anything, so
// every submodel is constructed once with its final settings.
type modelOptions struct {
	audioSrc            AudioSource
	vimMode             bool
	librespotInactiveCh <-chan struct{}
	tokenSaveErrCh      <-chan error
	tokenRevokedCh      <-chan struct{}
}

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
	return func(o *modelOptions) { o.audioSrc = src }
}

// WithVimMode enables vim-style keybindings (h/l for back/select, ctrl+d/u half-page, etc.).
func WithVimMode() ModelOption {
	return func(o *modelOptions) { o.vimMode = true }
}

// WithLibrespotInactive provides a channel that signals when librespot reports
// its device became inactive (playback moved to another device).
func WithLibrespotInactive(ch <-chan struct{}) ModelOption {
	return func(o *modelOptions) { o.librespotInactiveCh = ch }
}

// WithTokenSaveErrors provides a channel that emits non-fatal OAuth problems:
// token persistence failures and a failed refresh at startup. Each value is
// rendered as a visible warning so the user can tell why they're getting
// logged out between sessions or why the first requests fail.
func WithTokenSaveErrors(ch <-chan error) ModelOption {
	return func(o *modelOptions) { o.tokenSaveErrCh = ch }
}

// WithTokenRevoked provides a channel that fires once if Spotify rejects
// the refresh token as permanently invalid. A receive triggers a clean
// TUI shutdown (bootstrap.Run then prints a re-login message on stderr).
func WithTokenRevoked(ch <-chan struct{}) ModelOption {
	return func(o *modelOptions) { o.tokenRevokedCh = ch }
}
