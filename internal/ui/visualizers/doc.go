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
//     visuals like lyrics scroll).
//   - ImageAware receives the current track's album art (for AlbumArt).
//   - LyricsAware receives the track's lyric lines (for Lyrics).
//
// A visualizer opts in by implementing the matching capability interface;
// the ui package's visualizerModel pushes data to everything that opts in.
//
// # Current implementations
//
// Album art and lyrics panels work without an audio source. Spectrum,
// Oscillogram, Spectrogram, VU meter, Starfield, and the four
// Milkdrop-style shaders (Spiral, Tunnel, Kaleidoscope, Ripple) need
// real-time PCM, so they're only useful when librespot + pipe backend
// is active.
package visualizers
