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

	// volumePercent is the Spotify Connect device volume (1–100). When the
	// user turns volume down, librespot's softvol scales the PCM before
	// piping, so the FFT sees a quieter signal and visualizers dim. Latest()
	// compensates by applying the inverse gain (capped at maxVolumeGain) to
	// the returned frequency bands, keeping visualizers bright at low volumes.
	volumePercent atomic.Int32

	mu       sync.Mutex
	cancelFn func() // cancels the current read goroutine
	done     chan struct{}
	stopped  bool

	// NewPlayer creates an audio player. Defaults to oto. Override in tests.
	NewPlayer playerFactory
}

// maxVolumeGain caps the inverse-volume gain so very low volumes don't
// amplify background noise / FFT bin quantization into full-scale bands.
// 4x means volumes below 25% stop getting brighter.
const maxVolumeGain = 4.0

// NewPipeReader creates a PipeReader ready to accept pipes via Start().
func NewPipeReader() *PipeReader {
	pr := &PipeReader{
		NewPlayer: defaultPlayerFactory,
	}
	pr.volumePercent.Store(100)
	return pr
}

// SetVolumePercent records the current Spotify device volume (0–100) so
// Latest() can compensate the FFT bands for it. Safe to call from any
// goroutine; applies to the next Latest() read. Out-of-range values are
// clamped and logged so upstream data quality issues surface in the log.
func (pr *PipeReader) SetVolumePercent(v int) {
	clamped := v
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}
	if clamped != v {
		log.Printf("[pipe-reader] volume %d out of range, clamped to %d", v, clamped)
	}
	pr.volumePercent.Store(int32(clamped))
}

// Start begins reading PCM from pipe, playing audio and running FFT analysis.
// Safe to call multiple times — each call cancels the previous read loop.
// This handles librespot restarts which create a new stdout pipe each time.
func (pr *PipeReader) Start(pipe io.ReadCloser) {
	pr.mu.Lock()
	if pr.stopped {
		pr.mu.Unlock()
		pipe.Close()
		return
	}
	prevCancel := pr.cancelFn
	prevDone := pr.done

	done := make(chan struct{})
	quit := make(chan struct{})
	pr.done = done
	pr.cancelFn = func() { close(quit) }
	pr.mu.Unlock()

	// Wait for the previous read loop to exit without holding the mutex, so
	// a concurrent Stop() can still acquire it (and so we don't deadlock if
	// the previous loop is blocked on a hung player.IsPlaying()).
	if prevCancel != nil {
		prevCancel()
		<-prevDone
	}

	go pr.readLoop(pipe, quit, done)
}

// Latest returns the most recent FrequencyData, or nil if no fresh data.
// Returns nil if the last frame is older than 150ms (e.g., paused or between restarts).
// Thread-safe; called from the Bubble Tea goroutine.
//
// Bands are compensated by the inverse of the current device volume so
// visualizers stay bright at low playback volume (librespot's softvol
// scales the PCM before piping). Compensation is capped at maxVolumeGain.
func (pr *PipeReader) Latest() *FrequencyData {
	last := pr.lastUpdate.Load()
	if last == 0 || time.Since(time.Unix(0, last)) > 150*time.Millisecond {
		return nil
	}
	fd := pr.latest.Load()
	if fd == nil {
		return nil
	}
	gain := pr.volumeGain()
	if gain == 1.0 {
		return fd
	}
	out := *fd
	for i := range out.Bands {
		out.Bands[i] = clampUnit(out.Bands[i] * gain)
	}
	out.Peak = clampUnit(out.Peak * gain)
	out.Bass = clampUnit(out.Bass * gain)
	out.Mid = clampUnit(out.Mid * gain)
	out.High = clampUnit(out.High * gain)
	return &out
}

func (pr *PipeReader) volumeGain() float32 {
	v := pr.volumePercent.Load()
	if v >= 100 || v <= 0 {
		return 1.0
	}
	gain := 100.0 / float32(v)
	if gain > maxVolumeGain {
		gain = maxVolumeGain
	}
	return gain
}

func clampUnit(v float32) float32 {
	if v > 1.0 {
		return 1.0
	}
	if v < 0 {
		return 0
	}
	return v
}

// Stop cancels any active read loop. Safe to call multiple times.
func (pr *PipeReader) Stop() {
	pr.mu.Lock()
	if pr.stopped {
		pr.mu.Unlock()
		return
	}
	pr.stopped = true
	cancel := pr.cancelFn
	done := pr.done
	pr.cancelFn = nil
	pr.mu.Unlock()

	// Wait for the read loop to exit without holding the mutex, so a
	// hung readLoop (e.g. blocked in player.IsPlaying) can't deadlock
	// concurrent callers that need the lock.
	if cancel != nil {
		cancel()
		<-done
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

// Close satisfies the player interface. oto/v3 handles player cleanup
// internally (the Close method is deprecated and a no-op since v3.4),
// so we don't forward the call.
func (o *otoPlayer) Close() error    { return nil }
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
