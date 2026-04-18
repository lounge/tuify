// Package audio provides the PCM ingest and FFT analysis that feed the
// TUI's visualizers. PipeReader consumes raw little-endian s16le stereo
// samples from librespot's stdout; the FFT layer produces FrequencyData
// with log-spaced bands plus bass/mid/high convenience averages.
//
// FrequencyData carries a ProgressMs derived from the running sample
// count so visualizers can display playback progress without a separate
// Spotify poll. PipeReader is safe to call Start/Stop from any goroutine;
// reads are internally synchronized.
package audio
