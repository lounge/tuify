package visualizers

import (
	"math"
	"testing"

	"github.com/lounge/tuify/internal/audio"
)

const beatFrameMs = 50 // simulated frame spacing

var (
	beatSilent = [audio.NumBands]float32{}
	beatLoud   = func() [audio.NumBands]float32 {
		var b [audio.NumBands]float32
		for i := range b {
			b[i] = 1
		}
		return b
	}()
)

// driveBeats feeds bd one loud onset every spacingMs, starting at startMs,
// with silent frames every beatFrameMs in between (and before the first
// onset, so the first loud frame has a predecessor to diff against). It
// returns the number of onsets bd flagged (Pulse reset to 1) and the
// TempoMul observed after each onset. Stream time and playback position
// are fed the same millisecond value; driveBeatsQuantized is the variant
// with the whole-second position the app really delivers.
func driveBeats(t *testing.T, bd *beatDetector, startMs, spacingMs int32, beats int) (int, []float64) {
	t.Helper()
	return driveBeatsWith(t, bd, startMs, spacingMs, beats, func(ms int32) int32 { return ms })
}

// driveBeatsQuantized is driveBeats with the playback position advancing
// in 1000 ms steps, as nowPlayingModel reports it between polls, while
// stream time keeps millisecond resolution.
func driveBeatsQuantized(t *testing.T, bd *beatDetector, startMs, spacingMs int32, beats int) (int, []float64) {
	t.Helper()
	return driveBeatsWith(t, bd, startMs, spacingMs, beats, func(ms int32) int32 { return ms / 1000 * 1000 })
}

func driveBeatsWith(t *testing.T, bd *beatDetector, startMs, spacingMs int32, beats int, progress func(int32) int32) (int, []float64) {
	t.Helper()
	var detected int
	var tempos []float64
	for n := range int32(beats) {
		onset := startMs + n*spacingMs
		for p := onset - spacingMs + beatFrameMs; p < onset; p += beatFrameMs {
			bd.Tick(&beatSilent, int64(p), progress(p))
		}
		bd.Tick(&beatLoud, int64(onset), progress(onset))
		if bd.Pulse == 1.0 {
			detected++
		}
		tempos = append(tempos, bd.TempoMul)
	}
	return detected, tempos
}

func TestBeatDetector_Tempo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		spacingMs int32
		want      float64 // TempoMul once ≥3 intervals are in the history
	}{
		{"120bpm", 500, 1.0},
		{"150bpm", 400, 1.25},
		{"80bpm", 750, 80.0 / 120},
		{"240bpm clamps high", 250, 1.6},
		{"40bpm clamps low", 1500, 0.4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var bd beatDetector
			bd.Reset()
			const beats = 12
			detected, tempos := driveBeats(t, &bd, 2000, tt.spacingMs, beats)
			if detected != beats {
				t.Fatalf("detected %d onsets, want %d", detected, beats)
			}
			// Onsets 0–2 give at most 2 intervals: updateTempo needs 3, so
			// TempoMul keeps its Reset value.
			for i := range 3 {
				if tempos[i] != 1.0 {
					t.Errorf("TempoMul after onset %d = %v, want 1.0 (fewer than 3 intervals)", i, tempos[i])
				}
			}
			for i := 3; i < beats; i++ {
				if math.Abs(tempos[i]-tt.want) > 1e-9 {
					t.Errorf("TempoMul after onset %d = %v, want %v", i, tempos[i], tt.want)
				}
			}
			if len(bd.intervals) != beatMaxHistory {
				t.Errorf("interval history len = %d, want capped at %d", len(bd.intervals), beatMaxHistory)
			}
		})
	}
}

// The app reports the playback position in whole seconds, so intervals
// measured from it would all be 1000 or 2000 ms and pin the tempo at the
// 60 BPM value (0.5) or the floor. Intervals must come from stream time.
func TestBeatDetector_TempoWithSecondQuantizedProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		spacingMs int32
		want      float64
	}{
		{"120bpm", 500, 1.0},
		{"150bpm", 400, 1.25},
		{"80bpm", 750, 80.0 / 120},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var bd beatDetector
			bd.Reset()
			const beats = 12
			detected, tempos := driveBeatsQuantized(t, &bd, 2000, tt.spacingMs, beats)
			if detected != beats {
				t.Fatalf("detected %d onsets, want %d", detected, beats)
			}
			for i := 3; i < beats; i++ {
				if math.Abs(tempos[i]-tt.want) > 1e-9 {
					t.Errorf("TempoMul after onset %d = %v, want %v (intervals must be timed with stream ms, not the whole-second position)", i, tempos[i], tt.want)
				}
			}
			// The history must have filled with real intervals; with the
			// position as the clock every other interval is 0 and dropped.
			if len(bd.intervals) != beatMaxHistory {
				t.Errorf("interval history len = %d, want %d", len(bd.intervals), beatMaxHistory)
			}
			for _, iv := range bd.intervals {
				if iv != tt.spacingMs {
					t.Errorf("recorded interval %d ms, want %d", iv, tt.spacingMs)
				}
			}
		})
	}
}

// A seek is still noticed from the position while intervals come from
// stream time: stream time never jumps back, so the position is the only
// clock that can report one.
func TestBeatDetector_ResetOnSeekWithQuantizedProgress(t *testing.T) {
	t.Parallel()

	var bd beatDetector
	bd.Reset()
	driveBeatsQuantized(t, &bd, 10000, 400, 6)
	if bd.TempoMul != 1.25 || len(bd.intervals) != 5 {
		t.Fatalf("precondition: TempoMul=%v intervals=%d, want 1.25 and 5", bd.TempoMul, len(bd.intervals))
	}
	// Stream time keeps going; the position jumps back a second.
	bd.Tick(&beatSilent, int64(bd.lastBeatStreamMs+50), bd.lastBeatProgressMs-1000)
	if bd.hasBeat || len(bd.intervals) != 0 || bd.TempoMul != 1.0 {
		t.Fatalf("after seek: hasBeat=%v intervals=%d TempoMul=%v, want reset state", bd.hasBeat, len(bd.intervals), bd.TempoMul)
	}
}

// A stream restart (librespot relaunched) makes stream time go backwards
// once. That interval is skipped rather than recorded as a huge or
// negative value, and the history is kept.
func TestBeatDetector_StreamRestartSkipsInterval(t *testing.T) {
	t.Parallel()

	var bd beatDetector
	bd.Reset()
	driveBeats(t, &bd, 2000, 500, 12)
	if bd.TempoMul != 1.0 || len(bd.intervals) != beatMaxHistory {
		t.Fatalf("precondition: TempoMul=%v intervals=%d", bd.TempoMul, len(bd.intervals))
	}
	last := bd.lastBeatProgressMs
	// Position carries on at the same cadence; stream time restarts at 0.
	bd.Tick(&beatSilent, 50, last+450)
	bd.Tick(&beatLoud, 100, last+500)
	if bd.Pulse != 1.0 {
		t.Fatalf("Pulse = %v after onset, want 1.0", bd.Pulse)
	}
	if len(bd.intervals) != beatMaxHistory || bd.TempoMul != 1.0 {
		t.Errorf("after stream restart: intervals=%d TempoMul=%v, want history untouched (8, 1.0)", len(bd.intervals), bd.TempoMul)
	}
	for _, iv := range bd.intervals {
		if iv != 500 {
			t.Errorf("recorded interval %d ms after stream restart, want only 500 ms entries", iv)
		}
	}
}

func TestBeatDetector_TempoFollowsChange(t *testing.T) {
	t.Parallel()

	var bd beatDetector
	bd.Reset()
	// 12 onsets at 500 ms, then 8 at 400 ms: the 8-interval window then
	// holds only 400 ms intervals.
	driveBeats(t, &bd, 2000, 500, 12)
	last := 2000 + 11*int32(500)
	driveBeats(t, &bd, last+400, 400, 8)
	if math.Abs(bd.TempoMul-1.25) > 1e-9 {
		t.Errorf("TempoMul = %v after the window fills with 400 ms intervals, want 1.25", bd.TempoMul)
	}
}

func TestBeatDetector_ResetOnSeek(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		seekTo func(lastBeat int32) int32
	}{
		{"backwards", func(last int32) int32 { return last - 1000 }},
		{"forward >5s", func(last int32) int32 { return last + 5001 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var bd beatDetector
			bd.Reset()
			driveBeats(t, &bd, 10000, 400, 6)
			if bd.TempoMul != 1.25 || len(bd.intervals) != 5 {
				t.Fatalf("precondition: TempoMul=%v intervals=%d, want 1.25 and 5", bd.TempoMul, len(bd.intervals))
			}

			seek := tt.seekTo(bd.lastBeatProgressMs)
			bd.Tick(&beatSilent, int64(seek), seek)
			if len(bd.intervals) != 0 || bd.TempoMul != 1.0 || bd.hasBeat || bd.Pulse != 0 {
				t.Fatalf("after seek: intervals=%d TempoMul=%v hasBeat=%v Pulse=%v, want reset state",
					len(bd.intervals), bd.TempoMul, bd.hasBeat, bd.Pulse)
			}

			// History restarts: a new 250 ms tempo needs 3 fresh intervals
			// and is not averaged with the pre-seek 400 ms ones.
			_, tempos := driveBeats(t, &bd, seek+250, 250, 4)
			for i := range 3 {
				if tempos[i] != 1.0 {
					t.Errorf("TempoMul after post-seek onset %d = %v, want 1.0", i, tempos[i])
				}
			}
			if tempos[3] != 1.6 {
				t.Errorf("TempoMul after 3 post-seek intervals = %v, want 1.6", tempos[3])
			}
		})
	}
}

func TestBeatDetector_NoDoubleTrigger(t *testing.T) {
	t.Parallel()

	var bd beatDetector
	bd.Reset()
	bd.Tick(&beatSilent, 1000, 1000)
	bd.Tick(&beatLoud, 1050, 1050)
	if bd.Pulse != 1.0 {
		t.Fatalf("Pulse = %v after onset, want 1.0", bd.Pulse)
	}
	// A second, louder-still rise while flux stays above threshold must not
	// re-trigger: cooldown holds until flux drops below it.
	var louder [audio.NumBands]float32
	for i := range louder {
		louder[i] = 3
	}
	bd.Tick(&louder, 1100, 1100)
	if want := beatPulseDecay; bd.Pulse != want {
		t.Errorf("Pulse = %v on sustained rise, want decayed %v (no re-trigger)", bd.Pulse, want)
	}
}
