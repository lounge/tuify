// Package audio provides the PCM ingest and FFT analysis that feed the
// TUI's visualizers. PipeReader consumes raw little-endian s16le stereo
// samples from librespot's stdout; the FFT layer produces FrequencyData
// with log-spaced bands plus bass/mid/high convenience averages.
//
// FrequencyData also exposes LeftLevel and RightLevel, time-domain
// per-channel peak amplitudes shared-AGC normalized to 0–1, for
// visualizers (e.g. the VU meter) that need true stereo loudness rather
// than the post-mix spectral peak. ProgressMs is derived from the running
// sample count so visualizers can display playback progress without a
// separate Spotify poll. PipeReader is safe to call Start/Stop from any
// goroutine; reads are internally synchronized.
package audio
