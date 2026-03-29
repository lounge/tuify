package audio

// NumBands is the number of frequency bands in FFT output.
const NumBands = 64

// FrequencyData holds FFT output mapped to visualization-friendly bands.
type FrequencyData struct {
	Bands      [NumBands]float32 // log-spaced frequency bands, normalized 0.0–1.0
	Peak       float32           // overall peak amplitude this frame
	Bass       float32           // average of bands 0–7
	Mid        float32           // average of bands 8–31
	High       float32           // average of bands 32–63
	ProgressMs int32             // playback progress derived from PCM sample count
}

// Band boundary indices for convenience fields.
const (
	bassEnd = 8  // bands 0–7
	midEnd  = 32 // bands 8–31
	// bands 32–63 = high
)

// ComputeConvenienceFields fills Bass, Mid, and High from Bands.
func (fd *FrequencyData) ComputeConvenienceFields() {
	fd.Bass, fd.Mid, fd.High = 0, 0, 0
	for i := 0; i < bassEnd; i++ {
		fd.Bass += fd.Bands[i]
	}
	fd.Bass /= bassEnd

	for i := bassEnd; i < midEnd; i++ {
		fd.Mid += fd.Bands[i]
	}
	fd.Mid /= float32(midEnd - bassEnd)

	for i := midEnd; i < NumBands; i++ {
		fd.High += fd.Bands[i]
	}
	fd.High /= float32(NumBands - midEnd)
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
