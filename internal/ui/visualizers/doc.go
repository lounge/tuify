// Package visualizers holds the pluggable Visualizer implementations
// rendered on top of the now-playing view when the user toggles on the
// visualizer pane.
//
// # The Visualizer interface
//
// Each visualizer implements Visualizer: a small contract of Init (new
// track), Advance (per-frame tick), and View (render). Optional
// capability interfaces extend what a visualizer can see:
//
//   - AudioAware receives per-frame FrequencyData from the FFT pipeline.
//   - ProgressAware receives playback progress in ms (for time-aligned
//     visuals like lyrics scroll, and for beat tracking that must notice
//     seeks; FrequencyData.StreamMs is stream time, not position). The
//     progress arrives in whole-second steps, so anything that needs
//     finer timing between frames, such as the beat tracker's interval
//     measurement, takes that from StreamMs instead.
//   - ImageAware receives the current track's album art (for AlbumArt).
//   - LyricsAware receives the track's lyric lines as LyricLines, timed
//     when the source had timestamps, or is told the track is
//     instrumental (for Lyrics, which highlights the line being sung
//     from the timestamps and falls back to a proportional scroll).
//   - SizeAware receives the pane size when it is shown and on every
//     resize while it is, for state laid out by it (the Milkdrop
//     framebuffers). Init keeps the size.
//
// A visualizer opts in by implementing the matching capability interface;
// the ui package's visualizerModel pushes data to everything that opts in.
//
// View runs inside bubbletea's View and must not change what Advance or a
// later frame sees: a visualizer that needs its state sized implements
// SizeAware rather than resizing in View, and draws a frame of the
// requested size from whatever it has (Milkdrop: black) until sized.
// Scratch buffers and caches derived purely from the inputs are fine.
//
// # Current implementations
//
// Album art and lyrics panels work without an audio source. Spectrum,
// Oscillogram, Spectrogram, VU meter, Starfield, and the four
// Milkdrop-style shaders (Spiral, Tunnel, Kaleidoscope, Ripple) need
// real-time PCM, so they're only useful when librespot + pipe backend
// is active.
package visualizers
