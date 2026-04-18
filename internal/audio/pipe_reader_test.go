package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

// noopPlayer is a test player that reads src to completion without real audio.
type noopPlayer struct {
	src     io.Reader
	playing atomic.Bool
	done    chan struct{}
}

func newNoopPlayer(src io.Reader, _ PCMFormat) (player, error) {
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
		val := int16(16000 * math.Sin(2*math.Pi*freq*float64(i)/float64(DefaultFormat.SampleRate)))
		offset := i * 4
		binary.LittleEndian.PutUint16(buf[offset:], uint16(val))
		binary.LittleEndian.PutUint16(buf[offset+2:], uint16(val))
	}
	return buf
}

func TestPipeReader_LatestNilBeforeStart(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	if fd := pr.Latest(); fd != nil {
		t.Errorf("Latest before Start: got %+v, want nil", fd)
	}
}

func TestPipeReader_LatestNilWhenStale(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	// Manually set a stale timestamp (200ms ago).
	pr.lastUpdate.Store(time.Now().Add(-200 * time.Millisecond).UnixNano())
	fd := &FrequencyData{Peak: 0.5}
	pr.latest.Store(fd)

	if got := pr.Latest(); got != nil {
		t.Errorf("Latest should be nil for stale data, got %+v", got)
	}
}

func TestPipeReader_ReceivesFFTData(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	// Generate enough PCM for several FFT chunks.
	pcm := generateSineBytes(440.0, 4)
	pipe := io.NopCloser(bytes.NewReader(pcm))

	pr.Start(pipe)

	// Wait for processing to complete (pipe will EOF).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for FFT data")
		default:
		}
		if fd := pr.Latest(); fd != nil {
			// 440 Hz should produce non-zero energy.
			if fd.Peak <= 0 {
				t.Errorf("Peak should be > 0, got %f", fd.Peak)
			}
			pr.Stop()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPipeReader_StopIdempotent(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	// Stop without Start.
	pr.Stop()
	pr.Stop()

	// Stop after Start.
	pcm := generateSineBytes(440.0, 2)
	pipe := io.NopCloser(bytes.NewReader(pcm))

	pr2 := NewPipeReader()
	pr2.NewPlayer = newNoopPlayer
	pr2.Start(pipe)
	time.Sleep(50 * time.Millisecond)
	pr2.Stop()
	pr2.Stop()
}

func TestPipeReader_ReentrantStart(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	// Start with first pipe.
	pcm1 := generateSineBytes(440.0, 2)
	pipe1 := io.NopCloser(bytes.NewReader(pcm1))
	pr.Start(pipe1)
	time.Sleep(50 * time.Millisecond)

	// Start with second pipe — should cancel first.
	pcm2 := generateSineBytes(880.0, 4)
	pipe2 := io.NopCloser(bytes.NewReader(pcm2))
	pr.Start(pipe2)

	// Wait for second pipe to produce data.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for FFT data from second pipe")
		default:
		}
		if fd := pr.Latest(); fd != nil {
			pr.Stop()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPipeReader_StartAfterStopIgnored(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	pr.Stop()

	// Start after Stop should be a no-op (pipe gets closed).
	pcm := generateSineBytes(440.0, 2)
	pipe := io.NopCloser(bytes.NewReader(pcm))
	pr.Start(pipe)

	// Should have no data since Start was ignored.
	time.Sleep(100 * time.Millisecond)
	if fd := pr.Latest(); fd != nil {
		t.Error("expected nil after Start on stopped PipeReader")
	}
}

func TestPipeReader_ProgressMsAdvances(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	// 8 chunks at 44100 Hz = 8 * 2048 / 44100 ≈ 371 ms of audio.
	numChunks := 8
	pcm := generateSineBytes(440.0, numChunks)
	pipe := io.NopCloser(bytes.NewReader(pcm))

	pr.Start(pipe)

	// Wait for the pipe to be fully consumed.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for FFT data")
		default:
		}
		if fd := pr.Latest(); fd != nil {
			// After 8 chunks (8 * 2048 mono samples at 44100), progress should be ~371ms.
			expectedMs := int32(numChunks * WindowSize * 1000 / DefaultFormat.SampleRate)
			if fd.ProgressMs < expectedMs/2 {
				t.Errorf("ProgressMs = %d, want at least %d", fd.ProgressMs, expectedMs/2)
			}
			pr.Stop()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// seedFreshFrame stores a FrequencyData with a fresh timestamp so Latest()
// returns it without running the full pipe/FFT pipeline.
func seedFreshFrame(pr *PipeReader, fd *FrequencyData) {
	pr.latest.Store(fd)
	pr.lastUpdate.Store(time.Now().UnixNano())
}

func TestPipeReader_Latest_VolumeGainAt50Percent(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer
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
}

func TestPipeReader_Latest_VolumeGainCapsAt4x(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer
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
}

func TestPipeReader_Latest_Unity_NoCopy(t *testing.T) {
	// At 100% volume there's no gain to apply; Latest should return the
	// stored pointer directly (short-circuit path).
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer
	pr.SetVolumePercent(100)

	base := &FrequencyData{Peak: 0.42}
	seedFreshFrame(pr, base)

	got := pr.Latest()
	if got != base {
		t.Errorf("at 100%% volume Latest should return stored pointer; got new copy")
	}
}

// TestPipeReader_Latest_ConcurrentSetVolume verifies the race detector is
// happy with SetVolumePercent firing from one goroutine while Latest runs
// from another. Run with `go test -race`.
func TestPipeReader_Latest_ConcurrentSetVolume(t *testing.T) {
	pr := NewPipeReader()
	pr.NewPlayer = newNoopPlayer

	base := &FrequencyData{Peak: 0.5, Bass: 0.3, Mid: 0.2, High: 0.1}
	for i := range base.Bands {
		base.Bands[i] = 0.25
	}
	seedFreshFrame(pr, base)

	var stop atomic.Bool
	done := make(chan struct{}, 2)

	go func() {
		for v := 1; !stop.Load(); v = (v%100) + 1 {
			pr.SetVolumePercent(v)
		}
		done <- struct{}{}
	}()

	go func() {
		for !stop.Load() {
			if fd := pr.Latest(); fd != nil {
				// Touch fields so the race detector sees the read.
				_ = fd.Peak + fd.Bass + fd.Mid + fd.High
			}
			// Also refresh the timestamp occasionally so Latest keeps
			// returning non-nil.
			pr.lastUpdate.Store(time.Now().UnixNano())
		}
		done <- struct{}{}
	}()

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	<-done
	<-done
}
