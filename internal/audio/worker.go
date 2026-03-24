package audio

import (
	"encoding/binary"
	"io"
	"log"
	"net"

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
		// oto drives reads from the bridge internally.
		// We just wait.
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
}

func newPCMBridge(stdin io.Reader, analyzer *Analyzer, conn net.Conn, format PCMFormat) *pcmBridge {
	return &pcmBridge{
		stdin:    stdin,
		analyzer: analyzer,
		conn:     conn,
		format:   format,
	}
}

// Read implements io.Reader. oto calls this to get PCM data for playback.
// We read from stdin, run FFT on complete chunks, and send results over the socket.
func (b *pcmBridge) Read(p []byte) (int, error) {
	n, err := b.stdin.Read(p)
	if n <= 0 {
		return n, err
	}

	// Count mono samples for progress tracking.
	// Each sample frame is Channels * (BitDepth/8) bytes.
	bytesPerFrame := b.format.Channels * (b.format.BitDepth / 8)
	monoSamples := int64(n / bytesPerFrame)
	b.totalFrames += monoSamples

	// Run FFT when we have enough data for a full window.
	// We analyze in ChunkBytes increments.
	if n >= ChunkBytes {
		samples := make([]int16, WindowSize*2) // stereo
		for i := range WindowSize * 2 {
			if i*2+1 < n {
				samples[i] = int16(binary.LittleEndian.Uint16(p[i*2 : i*2+2]))
			}
		}

		fd := b.analyzer.Analyze(samples)
		fd.ProgressMs = int32(b.totalFrames * 1000 / int64(b.format.SampleRate))

		// Best-effort send; don't block playback on socket writes.
		_ = EncodeFrame(b.conn, &fd)
	}

	return n, err
}
