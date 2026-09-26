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
// TempoMul observed after each onset.
func driveBeats(t *testing.T, bd *beatDetector, startMs, spacingMs int32, beats int) (int, []float64) {
	t.Helper()
	var detected int
	var tempos []float64
	for n := range int32(beats) {
		onset := startMs + n*spacingMs
		for p := onset - spacingMs + beatFrameMs; p < onset; p += beatFrameMs {
			bd.Tick(&beatSilent, p)
		}
		bd.Tick(&beatLoud, onset)
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

			seek := tt.seekTo(bd.lastBeatMs)
			bd.Tick(&beatSilent, seek)
			if len(bd.intervals) != 0 || bd.TempoMul != 1.0 || bd.lastBeatMs != 0 || bd.Pulse != 0 {
				t.Fatalf("after seek: intervals=%d TempoMul=%v lastBeatMs=%d Pulse=%v, want reset state",
					len(bd.intervals), bd.TempoMul, bd.lastBeatMs, bd.Pulse)
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
	bd.Tick(&beatSilent, 1000)
	bd.Tick(&beatLoud, 1050)
	if bd.Pulse != 1.0 {
		t.Fatalf("Pulse = %v after onset, want 1.0", bd.Pulse)
	}
	// A second, louder-still rise while flux stays above threshold must not
	// re-trigger: cooldown holds until flux drops below it.
	var louder [audio.NumBands]float32
	for i := range louder {
		louder[i] = 3
	}
	bd.Tick(&louder, 1100)
	if want := beatPulseDecay; bd.Pulse != want {
		t.Errorf("Pulse = %v on sustained rise, want decayed %v (no re-trigger)", bd.Pulse, want)
	}
}
