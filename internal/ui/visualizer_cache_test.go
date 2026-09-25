package ui

import (
	"context"
	"testing"
	"time"
)

// TestAsyncLoader_LateCancelledResultDoesNotDropCurrent replays the race
// that left the lyrics pane empty after a track skip: the cancelled first
// fetch finishes after the second one has started and sends its stale
// result. The second fetch's result must still be delivered.
func TestAsyncLoader_LateCancelledResultDoesNotDropCurrent(t *testing.T) {
	l := newAsyncLoader[string]()

	ctx1, cancel1, ch1 := l.begin(context.Background(), time.Minute)
	defer cancel1()

	// Skip to the next track: the first fetch is cancelled.
	var drained []string
	l.drain(func(r string) { drained = append(drained, r) })
	_, cancel2, ch2 := l.begin(context.Background(), time.Minute)
	defer cancel2()

	if ctx1.Err() == nil {
		t.Fatal("begin did not cancel the previous operation")
	}

	// The cancelled fetch reports late, then the current one completes.
	// Sends are non-blocking so a regression fails instead of hanging.
	trySend(t, ch1, "stale")
	trySend(t, ch2, "current")

	l.drain(func(r string) { drained = append(drained, r) })
	if len(drained) != 1 || drained[0] != "current" {
		t.Fatalf("drained %q, want only the current result", drained)
	}
}

func TestAsyncLoader_BeginAppliesTimeout(t *testing.T) {
	l := newAsyncLoader[int]()
	ctx, cancel, _ := l.begin(context.Background(), time.Hour)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("begin context has no deadline")
	}
	if d := time.Until(deadline); d <= 59*time.Minute || d > time.Hour {
		t.Errorf("deadline in %v, want about 1h", d)
	}
}

func trySend[R any](t *testing.T, ch chan<- R, v R) {
	t.Helper()
	select {
	case ch <- v:
	default:
		t.Errorf("send of %v blocked: result slot already taken", v)
	}
}
