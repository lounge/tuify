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

	// newPlayer creates an audio player. Defaults to oto. Override in tests.
	newPlayer playerFactory

	// playerErrLogged makes the "no audio device" message a one-liner per
	// PipeReader. The oto context is a process-wide singleton, so a failed
	// init is permanent: every librespot restart would otherwise re-log it.
	playerErrLogged atomic.Bool
}

// maxVolumeGain caps the inverse-volume gain so very low volumes don't
// amplify background noise / FFT bin quantization into full-scale bands.
// 4x means volumes below 25% stop getting brighter.
const maxVolumeGain = 4.0

// NewPipeReader creates a PipeReader ready to accept pipes via Start().
func NewPipeReader() *PipeReader {
	pr := &PipeReader{
		newPlayer: defaultPlayerFactory,
	}
	pr.volumePercent.Store(100)
	return pr
}

// SetVolumePercent records the current Spotify device volume (0–100) so
// Latest() can compensate the FFT bands for it. Safe to call from any
// goroutine; applies to the next Latest() read. Out-of-range values are
// clamped and logged so upstream data quality issues surface in the log.
func (pr *PipeReader) SetVolumePercent(v int) {
	clamped := min(max(v, 0), 100)
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
	// Always hand out a copy. The published frame is shared across
	// goroutines, and visualizers keep the returned pointer across ticks;
	// a copy makes it impossible for a consumer write to race the
	// producer or other consumers. One small allocation per UI tick.
	out := *fd
	gain := pr.volumeGain()
	if gain == 1.0 {
		return &out
	}
	for i := range out.Bands {
		out.Bands[i] = clampUnit(out.Bands[i] * gain)
	}
	out.Peak = clampUnit(out.Peak * gain)
	out.Bass = clampUnit(out.Bass * gain)
	out.Mid = clampUnit(out.Mid * gain)
	out.High = clampUnit(out.High * gain)
	out.LeftLevel = clampUnit(out.LeftLevel * gain)
	out.RightLevel = clampUnit(out.RightLevel * gain)
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
//
// If no audio player can be created, the pipe is still drained through the
// FFT bridge by a silentPlayer, so visualizers keep getting frames. Closing
// the pipe instead would kill librespot with EPIPE, and its restart would
// hit the same (permanent) error forever.
func (pr *PipeReader) readLoop(pipe io.ReadCloser, quit <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	analyzer := newAnalyzer(WindowSize)
	format := defaultFormat

	bridge := &pipeReaderBridge{
		pipe:     pipe,
		analyzer: analyzer,
		format:   format,
		accum:    make([]byte, 0, ChunkBytes),
		store:    pr.storeFrame,
	}

	p, ok := pr.createPlayer(bridge, pipe, format, quit)
	if !ok {
		// quit fired while the player was still being created; the pipe is
		// closed and the creator will discard whatever it produces.
		return
	}

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

// createPlayer runs newPlayer without letting it hold up shutdown: the
// factory can block in audio-driver initialization (oto's ready channel),
// and Stop/Start wait for readLoop to exit. It returns ok=false when quit
// closed first; the pipe is then closed here, and the factory goroutine
// closes any player it eventually produces, since nobody else will.
//
// When the factory fails, a silentPlayer takes its place so the bridge is
// still drained (see readLoop).
func (pr *PipeReader) createPlayer(bridge *pipeReaderBridge, pipe io.Closer, format pCMFormat, quit <-chan struct{}) (player, bool) {
	type result struct {
		p   player
		err error
	}
	// Unbuffered on purpose: a send can only complete if readLoop is still
	// receiving, so exactly one side ends up owning the player.
	res := make(chan result)
	go func() {
		p, err := pr.newPlayer(bridge, format)
		select {
		case res <- result{p, err}:
		case <-quit:
			if p != nil {
				p.Close()
			}
		}
	}()

	var r result
	select {
	case r = <-res:
	case <-quit:
		log.Printf("[pipe-reader] cancelled while creating audio player, shutting down")
		pipe.Close()
		return nil, false
	}

	if r.err != nil {
		if pr.playerErrLogged.CompareAndSwap(false, true) {
			log.Printf("[pipe-reader] no audio device, playback is silent but visualizers stay live: %v", r.err)
		}
		return newSilentPlayer(bridge), true
	}
	log.Printf("[pipe-reader] playing")
	return r.p, true
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
	pipe     io.Reader
	analyzer *analyzer
	format   pCMFormat
	frames   int64   // mono sample frames analyzed so far, in whole chunks
	accum    []byte  // bytes not yet analyzed; compacted in place, never re-sliced from the front
	samples  []int16 // decoded chunk, reused across chunks
	store    func(*FrequencyData)
}

// Read implements io.Reader. oto calls this to get PCM data for playback.
// We accumulate data and run FFT when a full chunk is ready.
func (b *pipeReaderBridge) Read(p []byte) (int, error) {
	n, err := b.pipe.Read(p)
	if n <= 0 {
		return n, err
	}

	// Accumulate data for FFT analysis.
	b.accum = append(b.accum, p[:n]...)

	// Process all complete chunks in the accumulation buffer.
	if b.samples == nil {
		b.samples = make([]int16, WindowSize*2) // stereo
	}
	off := 0
	for len(b.accum)-off >= ChunkBytes {
		chunk := b.accum[off : off+ChunkBytes]
		for i := range b.samples {
			b.samples[i] = int16(binary.LittleEndian.Uint16(chunk[i*2 : i*2+2]))
		}

		fd := b.analyzer.Analyze(b.samples)
		// Stream time is counted in whole analyzed chunks, so it is exact
		// for the frame it stamps and does not depend on how the pipe's
		// bytes were split across reads. Counting the bytes of each read
		// instead would floor away a partial sample frame on every
		// unaligned read, and the stamps would drift behind the audio.
		b.frames += WindowSize
		fd.StreamMs = b.frames * 1000 / int64(b.format.SampleRate)

		b.store(&fd)

		off += ChunkBytes
	}
	// Move the partial chunk to the front so accum keeps its capacity and
	// the next append doesn't reallocate.
	if off > 0 {
		b.accum = b.accum[:copy(b.accum, b.accum[off:])]
	}

	return n, err
}

// --- oto player plumbing ---

// playerFactory creates an audio player from a PCM source. The returned
// Closer stops playback when closed. Replaceable in tests to avoid needing
// a real audio device.
type playerFactory func(src io.Reader, format pCMFormat) (player, error)

// player is the minimal interface for audio playback.
type player interface {
	io.Closer
	IsPlaying() bool
}

// silentPlayer is the fallback when no audio device is available: it reads
// src to exhaustion so the FFT bridge keeps publishing frames, and produces
// no sound. IsPlaying turns false once src returns an error (pipe closed).
type silentPlayer struct {
	playing atomic.Bool
	done    chan struct{}
}

func newSilentPlayer(src io.Reader) *silentPlayer {
	s := &silentPlayer{done: make(chan struct{})}
	s.playing.Store(true)
	go func() {
		defer func() {
			s.playing.Store(false)
			close(s.done)
		}()
		buf := make([]byte, ChunkBytes)
		for {
			if _, err := src.Read(buf); err != nil {
				return
			}
		}
	}()
	return s
}

func (s *silentPlayer) IsPlaying() bool { return s.playing.Load() }

// Close waits for the drain goroutine, which exits once src fails; readLoop
// closes the pipe before calling Close, matching the order oto needs.
func (s *silentPlayer) Close() error {
	<-s.done
	return nil
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
// We initialize it lazily on first use and reuse across restarts. A failed
// initialization is therefore permanent for the process; otoCtxErr keeps it
// observable so every later factory call fails the same way instead of
// using a dead context.
var (
	otoCtx     *oto.Context
	otoCtxOnce sync.Once
	otoCtxErr  error
)

// defaultPlayerFactory creates a real oto player using the singleton context.
func defaultPlayerFactory(src io.Reader, format pCMFormat) (player, error) {
	otoCtxOnce.Do(func() {
		var ready chan struct{}
		otoCtx, ready, otoCtxErr = oto.NewContext(&oto.NewContextOptions{
			SampleRate:   format.SampleRate,
			ChannelCount: format.Channels,
			Format:       oto.FormatSignedInt16LE,
		})
		if otoCtxErr != nil {
			return
		}
		// Driver init runs asynchronously; oto reports its outcome only
		// through Err after ready closes. A nil error from NewContext
		// alone says nothing about whether a device was opened.
		//
		// Should ready never close (a driver that hangs in init), this
		// Do blocks for the life of the process, and so does every later
		// factory call behind it: createPlayer abandons the goroutine
		// running the factory, so each librespot restart then parks one
		// more goroutine here. The growth is per restart, not one-off,
		// and bounded by the restart count; it is the accepted price of
		// never letting driver init hold up Stop.
		<-ready
		otoCtxErr = otoCtx.Err()
	})
	if otoCtxErr != nil {
		return nil, otoCtxErr
	}

	p := otoCtx.NewPlayer(src)
	p.SetBufferSize(ChunkBytes * 4) // ~185ms buffer
	p.Play()
	return &otoPlayer{p: p}, nil
}
