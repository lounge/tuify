package audio

import (
	"encoding/binary"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
)

// PipeReader reads raw PCM from librespot's stdout pipe, plays it via oto,
// runs FFT analysis, and stores the latest FrequencyData atomically.
type PipeReader struct {
	latest     atomic.Pointer[FrequencyData]
	lastUpdate atomic.Int64 // unix nanos of last frame

	mu       sync.Mutex
	cancelFn func() // cancels the current read goroutine
	done     chan struct{}
	stopped  bool

	// NewPlayer creates an audio player. Defaults to oto. Override in tests.
	NewPlayer playerFactory
}

// NewPipeReader creates a PipeReader ready to accept pipes via Start().
func NewPipeReader() *PipeReader {
	return &PipeReader{
		NewPlayer: defaultPlayerFactory,
	}
}

// Start begins reading PCM from pipe, playing audio and running FFT analysis.
// Safe to call multiple times — each call cancels the previous read loop.
// This handles librespot restarts which create a new stdout pipe each time.
func (pr *PipeReader) Start(pipe io.ReadCloser) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.stopped {
		pipe.Close()
		return
	}

	// Cancel any previous read loop.
	if pr.cancelFn != nil {
		pr.cancelFn()
		<-pr.done // wait for previous goroutine to exit
	}

	done := make(chan struct{})
	pr.done = done

	quit := make(chan struct{})
	pr.cancelFn = func() { close(quit) }

	go pr.readLoop(pipe, quit, done)
}

// Latest returns the most recent FrequencyData, or nil if no fresh data.
// Returns nil if the last frame is older than 150ms (e.g., paused or between restarts).
// Thread-safe; called from the Bubble Tea goroutine.
func (pr *PipeReader) Latest() *FrequencyData {
	last := pr.lastUpdate.Load()
	if last == 0 || time.Since(time.Unix(0, last)) > 150*time.Millisecond {
		return nil
	}
	return pr.latest.Load()
}

// Stop cancels any active read loop. Safe to call multiple times.
func (pr *PipeReader) Stop() {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.stopped {
		return
	}
	pr.stopped = true

	if pr.cancelFn != nil {
		pr.cancelFn()
		<-pr.done
		pr.cancelFn = nil
	}
}

// readLoop reads PCM from the pipe, plays audio, and runs FFT.
// Exits when the pipe closes (librespot died) or quit is closed (Stop/new Start).
func (pr *PipeReader) readLoop(pipe io.ReadCloser, quit <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	analyzer := NewAnalyzer(WindowSize)
	format := DefaultFormat

	bridge := &pipeReaderBridge{
		pipe:     pipe,
		analyzer: analyzer,
		format:   format,
		accum:    make([]byte, 0, ChunkBytes),
		store:    pr.storeFrame,
	}

	p, err := pr.NewPlayer(bridge, format)
	if err != nil {
		pipe.Close()
		log.Printf("[pipe-reader] failed to create audio player: %v", err)
		return
	}

	log.Printf("[pipe-reader] playing")

	// Block until player finishes (pipe EOF/error) or we're told to quit.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for p.IsPlaying() {
		select {
		case <-quit:
			log.Printf("[pipe-reader] cancelled, shutting down")
			// Close pipe first to unblock any in-flight bridge.Read(),
			// then close the player. Reversed order would deadlock.
			pipe.Close()
			p.Close()
			return
		case <-ticker.C:
		}
	}

	// Pipe closed naturally (librespot exited) — clean up.
	pipe.Close()
	p.Close()
	log.Printf("[pipe-reader] pipe closed, shutting down")
}

// storeFrame atomically publishes a new FrequencyData frame.
func (pr *PipeReader) storeFrame(fd *FrequencyData) {
	pr.latest.Store(fd)
	pr.lastUpdate.Store(time.Now().UnixNano())
}

// --- pipeReaderBridge ---

// pipeReaderBridge implements io.Reader for oto. It tees audio data to the
// FFT analyzer and stores frequency data via the store callback.
type pipeReaderBridge struct {
	pipe        io.Reader
	analyzer    *Analyzer
	format      PCMFormat
	totalFrames int64 // mono samples read so far
	accum       []byte
	store       func(*FrequencyData)
}

// Read implements io.Reader. oto calls this to get PCM data for playback.
// We accumulate data and run FFT when a full chunk is ready.
func (b *pipeReaderBridge) Read(p []byte) (int, error) {
	n, err := b.pipe.Read(p)
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

		b.store(&fd)

		b.accum = b.accum[ChunkBytes:]
	}

	return n, err
}

// --- oto player plumbing ---

// playerFactory creates an audio player from a PCM source. The returned
// Closer stops playback when closed. Replaceable in tests to avoid needing
// a real audio device.
type playerFactory func(src io.Reader, format PCMFormat) (player, error)

// player is the minimal interface for audio playback.
type player interface {
	io.Closer
	IsPlaying() bool
}

// otoPlayer wraps an oto.Player to satisfy the player interface.
type otoPlayer struct {
	p *oto.Player
}

func (o *otoPlayer) Close() error    { return o.p.Close() }
func (o *otoPlayer) IsPlaying() bool { return o.p.IsPlaying() }

// oto.NewContext is a process-wide singleton — it must only be called once.
// We initialize it lazily on first use and reuse across restarts.
var (
	otoCtx     *oto.Context
	otoCtxOnce sync.Once
	otoCtxErr  error
)

// defaultPlayerFactory creates a real oto player using the singleton context.
func defaultPlayerFactory(src io.Reader, format PCMFormat) (player, error) {
	otoCtxOnce.Do(func() {
		var ready chan struct{}
		otoCtx, ready, otoCtxErr = oto.NewContext(&oto.NewContextOptions{
			SampleRate:   format.SampleRate,
			ChannelCount: format.Channels,
			Format:       oto.FormatSignedInt16LE,
		})
		if otoCtxErr == nil {
			<-ready
		}
	})
	if otoCtxErr != nil {
		return nil, otoCtxErr
	}

	p := otoCtx.NewPlayer(src)
	p.SetBufferSize(ChunkBytes * 4) // ~185ms buffer
	p.Play()
	return &otoPlayer{p: p}, nil
}
