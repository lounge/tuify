package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// noopPlayer is a test player that reads src to completion without real audio.
type noopPlayer struct {
	src     io.Reader
	playing atomic.Bool
	done    chan struct{}
}

func newNoopPlayer(src io.Reader, _ pCMFormat) (player, error) {
	p := &noopPlayer{src: src, done: make(chan struct{})}
	p.playing.Store(true)
	go func() {
		defer func() {
			p.playing.Store(false)
			close(p.done)
		}()
		buf := make([]byte, 4096)
		for {
			_, err := src.Read(buf)
			if err != nil {
				return
			}
		}
	}()
	return p, nil
}

func (p *noopPlayer) IsPlaying() bool { return p.playing.Load() }
func (p *noopPlayer) Close() error {
	<-p.done
	return nil
}

// generateSineBytes produces raw PCM bytes for a sine wave.
func generateSineBytes(freq float64, numChunks int) []byte {
	totalSamples := numChunks * WindowSize
	buf := make([]byte, totalSamples*2*2) // stereo, 16-bit
	for i := range totalSamples {
		val := int16(16000 * math.Sin(2*math.Pi*freq*float64(i)/float64(defaultFormat.SampleRate)))
		offset := i * 4
		binary.LittleEndian.PutUint16(buf[offset:], uint16(val))
		binary.LittleEndian.PutUint16(buf[offset+2:], uint16(val))
	}
	return buf
}

// Every test that reads Latest runs in a synctest bubble, even when it never
// starts a read loop: Latest treats a frame older than 150ms as stale, and
// only the bubble's standing clock makes "fresh" a fact rather than a race
// against the scheduler under -race.

func TestPipeReader_LatestNilBeforeStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer

		if fd := pr.Latest(); fd != nil {
			t.Errorf("Latest before Start: got %+v, want nil", fd)
		}
	})
}

func TestPipeReader_LatestNilWhenStale(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer

		// Manually set a stale timestamp (200ms ago).
		pr.lastUpdate.Store(time.Now().Add(-200 * time.Millisecond).UnixNano())
		fd := &FrequencyData{Peak: 0.5}
		pr.latest.Store(fd)

		if got := pr.Latest(); got != nil {
			t.Errorf("Latest should be nil for stale data, got %+v", got)
		}
	})
}

// The pipe tests run in a synctest bubble: the pipe is in memory and the
// noop player drains it, so synctest.Wait returns once the read loop has
// published every frame and is idle. The fake clock also stands still, so
// Latest never sees those frames go stale mid-assertion.

// streamMsAfter is the StreamMs of the last frame of a chunks-long pipe.
func streamMsAfter(chunks int) int64 {
	return int64(chunks * WindowSize * 1000 / defaultFormat.SampleRate)
}

func TestPipeReader_ReceivesFFTData(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		defer pr.Stop()

		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(440.0, 4))))
		synctest.Wait()

		fd := pr.Latest()
		if fd == nil {
			t.Fatal("no FFT data after the pipe was drained")
		}
		// 440 Hz should produce non-zero energy.
		if fd.Peak <= 0 {
			t.Errorf("Peak should be > 0, got %f", fd.Peak)
		}
	})
}

func TestPipeReader_StopIdempotent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Stop without Start.
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		pr.Stop()
		pr.Stop()

		// Stop after Start, once the read loop is running.
		pr2 := NewPipeReader()
		pr2.newPlayer = newNoopPlayer
		pr2.Start(io.NopCloser(bytes.NewReader(generateSineBytes(440.0, 2))))
		synctest.Wait()
		pr2.Stop()
		pr2.Stop()
	})
}

func TestPipeReader_ReentrantStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		defer pr.Stop()

		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(440.0, 2))))
		synctest.Wait()

		// Start with a second, longer pipe — must cancel the first loop
		// and publish frames from the second one.
		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(880.0, 4))))
		synctest.Wait()

		fd := pr.Latest()
		if fd == nil {
			t.Fatal("no FFT data from the second pipe")
		}
		if want := streamMsAfter(4); fd.StreamMs != want {
			t.Errorf("StreamMs = %d, want %d (the second pipe's last frame)", fd.StreamMs, want)
		}
	})
}

// closeTracker records whether Close was called.
type closeTracker struct {
	io.Reader
	closed atomic.Bool
}

func (c *closeTracker) Close() error {
	c.closed.Store(true)
	return nil
}

func TestPipeReader_StartAfterStopIgnored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		pr.Stop()

		// Start after Stop should be a no-op that closes the pipe.
		pipe := &closeTracker{Reader: bytes.NewReader(generateSineBytes(440.0, 2))}
		pr.Start(pipe)
		synctest.Wait()

		if fd := pr.Latest(); fd != nil {
			t.Error("expected nil after Start on stopped PipeReader")
		}
		if !pipe.closed.Load() {
			t.Error("Start on a stopped PipeReader should close the pipe")
		}
	})
}

func TestPipeReader_StreamMsAdvances(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		defer pr.Stop()

		// 8 chunks at 44100 Hz = 8 * 2048 / 44100 ≈ 371 ms of audio.
		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(440.0, 8))))
		synctest.Wait()

		fd := pr.Latest()
		if fd == nil {
			t.Fatal("no FFT data after the pipe was drained")
		}
		if want := streamMsAfter(8); fd.StreamMs != want {
			t.Errorf("StreamMs = %d, want %d", fd.StreamMs, want)
		}
	})
}

// countingLog is a log sink that counts the lines containing a substring.
type countingLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *countingLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func (c *countingLog) count(substr string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, l := range c.lines {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

// When the player factory fails (no audio device), the pipe must still be
// drained through the FFT so visualizers get frames, and the failure must
// be logged once per PipeReader, not once per librespot restart. Closing
// the pipe instead would make librespot die on EPIPE and restart forever.
//
// Not parallel: it redirects the global logger.
func TestPipeReader_PlayerFailureDrainsPipeSilently(t *testing.T) {
	var logs countingLog
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = func(io.Reader, pCMFormat) (player, error) {
			return nil, errors.New("no audio device")
		}
		defer pr.Stop()

		first := bytes.NewReader(generateSineBytes(440.0, 4))
		pr.Start(io.NopCloser(first))
		synctest.Wait()

		if first.Len() != 0 {
			t.Errorf("%d bytes left unread: the pipe was not drained", first.Len())
		}
		fd := pr.Latest()
		if fd == nil {
			t.Fatal("no FFT data: the bridge was not fed without a player")
		}
		if fd.Peak <= 0 {
			t.Errorf("Peak = %f, want > 0", fd.Peak)
		}
		if want := streamMsAfter(4); fd.StreamMs != want {
			t.Errorf("StreamMs = %d, want %d (the last frame of the pipe)", fd.StreamMs, want)
		}

		// A librespot restart hands over a new pipe; the same permanent
		// error must not be logged again.
		second := bytes.NewReader(generateSineBytes(880.0, 2))
		pr.Start(io.NopCloser(second))
		synctest.Wait()
		if second.Len() != 0 {
			t.Errorf("%d bytes of the second pipe left unread", second.Len())
		}
		if got := logs.count("no audio device"); got != 1 {
			t.Errorf("player failure logged %d times, want exactly 1:\n%s", got, strings.Join(logs.lines, "\n"))
		}
	})
}

// blockingPlayer is what a factory stuck in driver initialization
// eventually returns; it records whether anyone closed it.
type blockingPlayer struct {
	closed atomic.Bool
}

func (p *blockingPlayer) IsPlaying() bool { return !p.closed.Load() }
func (p *blockingPlayer) Close() error {
	p.closed.Store(true)
	return nil
}

// Stop must return while the player factory is still blocked (oto's
// context init can hang on a broken audio stack), and the player the
// factory produces afterwards must be closed by someone, since the read
// loop that asked for it is gone. If Stop blocked on the factory, the
// bubble would deadlock and synctest would fail the test.
func TestPipeReader_StopWhilePlayerCreationBlocks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		created := &blockingPlayer{}
		pr := NewPipeReader()
		pr.newPlayer = func(io.Reader, pCMFormat) (player, error) {
			<-release
			return created, nil
		}

		pipe := &closeTracker{Reader: bytes.NewReader(generateSineBytes(440.0, 2))}
		pr.Start(pipe)
		synctest.Wait()

		pr.Stop()

		if !pipe.closed.Load() {
			t.Error("Stop returned without closing the pipe")
		}
		if created.closed.Load() {
			t.Fatal("player closed before the factory returned it")
		}

		close(release)
		synctest.Wait()
		if !created.closed.Load() {
			t.Error("player created after Stop was never closed")
		}
	})
}

// A new Start while the previous loop's factory is blocked must not wait
// for it either: librespot restarts hand over a new pipe regardless of the
// audio stack's state.
func TestPipeReader_RestartWhilePlayerCreationBlocks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var calls atomic.Int32
		pr := NewPipeReader()
		pr.newPlayer = func(src io.Reader, f pCMFormat) (player, error) {
			if calls.Add(1) == 1 {
				<-release
				return &blockingPlayer{}, nil
			}
			return newNoopPlayer(src, f)
		}
		defer pr.Stop()

		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(440.0, 2))))
		synctest.Wait()

		pr.Start(io.NopCloser(bytes.NewReader(generateSineBytes(880.0, 4))))
		synctest.Wait()

		fd := pr.Latest()
		if fd == nil {
			t.Fatal("no FFT data from the second pipe")
		}
		if want := streamMsAfter(4); fd.StreamMs != want {
			t.Errorf("StreamMs = %d, want %d (the second pipe's last frame)", fd.StreamMs, want)
		}
		close(release)
	})
}

// seedFreshFrame stores a FrequencyData with a fresh timestamp so Latest()
// returns it without running the full pipe/FFT pipeline.
func seedFreshFrame(pr *PipeReader, fd *FrequencyData) {
	pr.latest.Store(fd)
	pr.lastUpdate.Store(time.Now().UnixNano())
}

func TestPipeReader_Latest_VolumeGainAt50Percent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		pr.SetVolumePercent(50)

		base := &FrequencyData{
			Peak: 0.3,
			Bass: 0.2,
			Mid:  0.1,
			High: 0.05,
		}
		base.Bands[0] = 0.25
		base.Bands[10] = 0.4
		seedFreshFrame(pr, base)

		got := pr.Latest()
		if got == nil {
			t.Fatal("Latest returned nil")
		}
		// At 50% volume, gain = 100/50 = 2.0 — each band doubles.
		if math.Abs(float64(got.Peak)-0.6) > 1e-5 {
			t.Errorf("Peak: got %f, want 0.6", got.Peak)
		}
		if math.Abs(float64(got.Bass)-0.4) > 1e-5 {
			t.Errorf("Bass: got %f, want 0.4", got.Bass)
		}
		if math.Abs(float64(got.Bands[0])-0.5) > 1e-5 {
			t.Errorf("Bands[0]: got %f, want 0.5", got.Bands[0])
		}
		if math.Abs(float64(got.Bands[10])-0.8) > 1e-5 {
			t.Errorf("Bands[10]: got %f, want 0.8", got.Bands[10])
		}
		// Stored frame should be unchanged (Latest returns a gain-adjusted copy).
		if math.Abs(float64(base.Peak)-0.3) > 1e-5 {
			t.Errorf("source frame mutated: Peak = %f, want 0.3", base.Peak)
		}
	})
}

func TestPipeReader_Latest_VolumeGainCapsAt4x(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		pr.SetVolumePercent(10) // 100/10 = 10x uncapped, must clamp to 4x

		base := &FrequencyData{Peak: 0.1, Bass: 0.2, Mid: 0.3, High: 0.05}
		base.Bands[0] = 0.15
		base.Bands[5] = 0.3 // 0.3 * 4 = 1.2, should clamp at 1.0
		seedFreshFrame(pr, base)

		got := pr.Latest()
		if got == nil {
			t.Fatal("Latest returned nil")
		}
		// Gain should be exactly 4x, not 10x.
		if math.Abs(float64(got.Peak)-0.4) > 1e-5 {
			t.Errorf("Peak with gain cap: got %f, want 0.4 (0.1 * 4)", got.Peak)
		}
		// Bass at 0.2 * 4 = 0.8, under the 1.0 clamp.
		if math.Abs(float64(got.Bass)-0.8) > 1e-5 {
			t.Errorf("Bass with gain cap: got %f, want 0.8", got.Bass)
		}
		// Mid at 0.3 * 4 = 1.2, must clamp to 1.0.
		if got.Mid != 1.0 {
			t.Errorf("Mid clamp: got %f, want 1.0", got.Mid)
		}
		// Bands[5] at 0.3 * 4 = 1.2, must clamp to 1.0.
		if got.Bands[5] != 1.0 {
			t.Errorf("Bands[5] clamp: got %f, want 1.0", got.Bands[5])
		}
	})
}

func TestPipeReader_Latest_ReturnsCallerOwnedCopy(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Even at 100% volume, where no gain applies, Latest must return a copy:
		// visualizers keep the pointer across ticks, and a write through it
		// must never reach the shared published frame.
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer
		pr.SetVolumePercent(100)

		base := &FrequencyData{Peak: 0.42}
		base.Bands[0] = 0.5
		seedFreshFrame(pr, base)

		got := pr.Latest()
		if got == base {
			t.Fatal("Latest returned the published frame, not a copy")
		}
		if got.Peak != 0.42 || got.Bands[0] != 0.5 {
			t.Errorf("copy differs from the published frame: %+v", got)
		}
		got.Bands[0] = 1
		got.Peak = 1
		if base.Bands[0] != 0.5 || base.Peak != 0.42 {
			t.Error("writing through Latest's result mutated the published frame")
		}
	})
}

// TestPipeReader_Latest_ConcurrentSetVolume verifies the race detector is
// happy with SetVolumePercent firing from one goroutine while Latest runs
// from another. Run with `go test -race`.
func TestPipeReader_Latest_ConcurrentSetVolume(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		pr := NewPipeReader()
		pr.newPlayer = newNoopPlayer

		base := &FrequencyData{Peak: 0.5, Bass: 0.3, Mid: 0.2, High: 0.1}
		for i := range base.Bands {
			base.Bands[i] = 0.25
		}
		seedFreshFrame(pr, base)

		// A fixed iteration count rather than a wall-clock window keeps the
		// amount of overlap independent of machine speed.
		const iterations = 20000
		done := make(chan struct{}, 2)

		go func() {
			for i := range iterations {
				pr.SetVolumePercent(i%100 + 1)
			}
			done <- struct{}{}
		}()

		go func() {
			for range iterations {
				if fd := pr.Latest(); fd != nil {
					// Touch fields so the race detector sees the read.
					_ = fd.Peak + fd.Bass + fd.Mid + fd.High
				}
				// Keep the frame fresh so Latest keeps returning non-nil.
				pr.lastUpdate.Store(time.Now().UnixNano())
			}
			done <- struct{}{}
		}()

		<-done
		<-done
	})
}

// TestBridgeRead_OddReadSizesMatchWholeChunks feeds the bridge reads that
// never line up with a chunk boundary, so partial chunks carry over
// between reads, and checks it publishes the same frames as analyzing
// each whole chunk directly.
func TestBridgeRead_OddReadSizesMatchWholeChunks(t *testing.T) {
	t.Parallel()

	const chunks = 5
	raw := generateSineBytes(440, chunks)

	var got []FrequencyData
	br := &pipeReaderBridge{
		pipe:     bytes.NewReader(raw),
		analyzer: newAnalyzer(WindowSize),
		format:   defaultFormat,
		store:    func(fd *FrequencyData) { got = append(got, *fd) },
	}
	p := make([]byte, 3001)
	for {
		if _, err := br.Read(p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != chunks {
		t.Fatalf("published %d frames, want %d", len(got), chunks)
	}

	ref := newAnalyzer(WindowSize)
	samples := make([]int16, WindowSize*2)
	for c := range chunks {
		chunk := raw[c*ChunkBytes : (c+1)*ChunkBytes]
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(chunk[i*2:]))
		}
		want := ref.Analyze(samples)
		if got[c].Bands != want.Bands {
			t.Errorf("frame %d bands differ from a direct Analyze of the same chunk", c)
		}
		// Exact: stream time counts analyzed chunks, so the 3001-byte reads
		// (750.25 sample frames each) must not floor a quarter frame away
		// on every read and drift the stamps behind the audio.
		if wantMs := streamMsAfter(c + 1); got[c].StreamMs != wantMs {
			t.Errorf("frame %d StreamMs = %d, want %d", c, got[c].StreamMs, wantMs)
		}
	}
}
