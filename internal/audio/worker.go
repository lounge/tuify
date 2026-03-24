package audio

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"time"

	"github.com/ebitengine/oto/v3"
)

// Worker reads PCM from stdin, plays it via oto, and sends FFT data over a unix socket.
type Worker struct {
	Format     PCMFormat
	SocketPath string
}

// Run is the main loop. It blocks until stdin is closed or an error occurs.
func (w *Worker) Run(stdin io.Reader) error {
	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   w.Format.SampleRate,
		ChannelCount: w.Format.Channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		return err
	}
	<-ready

	conn, err := net.Dial("unix", w.SocketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	analyzer := NewAnalyzer(WindowSize)

	// Ring buffer for oto: we write PCM chunks to the player.
	player := otoCtx.NewPlayer(newPCMBridge(stdin, analyzer, conn, w.Format))
	player.SetBufferSize(ChunkBytes * 4) // ~185ms buffer
	player.Play()

	log.Printf("[audio-worker] playing, socket=%s", w.SocketPath)

	// Block until player finishes (stdin EOF).
	for player.IsPlaying() {
		time.Sleep(50 * time.Millisecond)
	}

	log.Printf("[audio-worker] stdin closed, shutting down")
	return nil
}

// pcmBridge implements io.Reader for oto. It tees audio data to the FFT analyzer
// and sends frequency data over the socket.
type pcmBridge struct {
	stdin       io.Reader
	analyzer    *Analyzer
	conn        net.Conn
	format      PCMFormat
	totalFrames int64 // mono samples read so far
	accum       []byte
}

func newPCMBridge(stdin io.Reader, analyzer *Analyzer, conn net.Conn, format PCMFormat) *pcmBridge {
	return &pcmBridge{
		stdin:    stdin,
		analyzer: analyzer,
		conn:     conn,
		format:   format,
		accum:    make([]byte, 0, ChunkBytes),
	}
}

// Read implements io.Reader. oto calls this to get PCM data for playback.
// We accumulate data and run FFT when a full chunk is ready.
func (b *pcmBridge) Read(p []byte) (int, error) {
	n, err := b.stdin.Read(p)
	if n <= 0 {
		return n, err
	}

	// Count mono samples for progress tracking.
	bytesPerFrame := b.format.Channels * (b.format.BitDepth / 8)
	monoSamples := int64(n / bytesPerFrame)
	b.totalFrames += monoSamples

	// Accumulate data for FFT analysis.
	b.accum = append(b.accum, p[:n]...)

	// Process all complete chunks in the accumulation buffer.
	for len(b.accum) >= ChunkBytes {
		chunk := b.accum[:ChunkBytes]
		samples := make([]int16, WindowSize*2) // stereo
		for i := range WindowSize * 2 {
			samples[i] = int16(binary.LittleEndian.Uint16(chunk[i*2 : i*2+2]))
		}

		fd := b.analyzer.Analyze(samples)
		fd.ProgressMs = int32(b.totalFrames * 1000 / int64(b.format.SampleRate))

		// Best-effort send; don't block playback on socket writes.
		_ = EncodeFrame(b.conn, &fd)

		b.accum = b.accum[ChunkBytes:]
	}

	return n, err
}
