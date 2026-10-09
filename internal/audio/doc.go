// Package audio provides the PCM ingest and FFT analysis that feed the
// TUI's visualizers. PipeReader consumes raw little-endian s16le stereo
// samples from librespot's stdout; the FFT layer produces FrequencyData
// with log-spaced bands plus bass/mid/high convenience averages.
//
// The FFT analyzer is an in-package radix-2 FFT, so its window size must be
// a power of two. It reuses its own buffers and band-to-bin map across
// frames, so analysis does not allocate; one analyzer is not safe for
// concurrent use.
//
// FrequencyData also exposes LeftLevel and RightLevel, time-domain
// per-channel peak amplitudes shared-AGC normalized to 0–1, for
// visualizers (e.g. the VU meter) that need true stereo loudness rather
// than the post-mix spectral peak. StreamMs is derived from the running
// sample count: it is the stream time of the current pipe, not the playback
// position (it is never reset by a track change or seek), so it suits
// time-based effects but not anything that must line up with the track.
// PipeReader is safe to call Start/Stop from any goroutine; reads are
// internally synchronized.
//
// When no audio device can be opened, PipeReader logs that once and keeps
// draining the pipe through the FFT without playing it, so visualizers
// stay live and librespot is not killed by a closed pipe. Player creation
// never holds up Stop: a factory blocked in driver initialization is
// abandoned, and whatever it eventually produces is closed.
//
// Frames are immutable once published. Latest returns a copy owned by the
// caller, so a visualizer may keep or modify it without affecting the
// producer or other consumers.
package audio
