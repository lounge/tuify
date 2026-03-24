package audio

// FrequencyData holds FFT output mapped to visualization-friendly bands.
type FrequencyData struct {
	Bands      [64]float32 // 64 log-spaced frequency bands, normalized 0.0–1.0
	Peak       float32     // overall peak amplitude this frame
	Bass       float32     // average of bands 0–7
	Mid        float32     // average of bands 8–31
	High       float32     // average of bands 32–63
	ProgressMs int32       // playback progress derived from PCM sample count
}

// PCMFormat describes the expected audio format from librespot.
type PCMFormat struct {
	SampleRate int
	Channels   int
	BitDepth   int
}

// DefaultFormat is librespot's default output: 44100 Hz, stereo, 16-bit signed LE.
var DefaultFormat = PCMFormat{SampleRate: 44100, Channels: 2, BitDepth: 16}

// WindowSize is the number of mono samples per FFT frame.
// 2048 at 44100 Hz = ~46 ms, a good latency/resolution tradeoff.
const WindowSize = 2048

// ChunkBytes is the number of raw bytes per FFT frame (stereo, 16-bit).
// 2048 samples × 2 channels × 2 bytes = 8192.
const ChunkBytes = WindowSize * 2 * 2
