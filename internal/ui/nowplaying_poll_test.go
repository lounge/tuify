package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// The HTTP-backed tests here run in a synctest bubble against an in-memory
// httptest server. "The request is in flight" and "the command returned"
// are then exact states reached through synctest.Wait, not guesses behind
// a wall-clock timeout, and the rate-limit cooldown runs on the fake clock.

// Cancelling the root context must cascade: any in-flight pollState()
// command sees the cancellation and returns playerStateMsg with a
// context.Canceled error instead of waiting for the per-op timeout. Proves
// the ctx threading from bootstrap.Run is wired up correctly for the
// now-playing poll path.
func TestPollState_RootContextCancelCascadesToHTTPCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Blocking server: holds the request open until its own request
		// context is cancelled. Our cancel() must trigger that.
		srv, entered := newBlockingServer(t)

		httpClient := &http.Client{Transport: &testutil.RewriteTransport{
			Base:   srv.Client().Transport,
			Target: srv.URL,
		}}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		np := &nowPlayingModel{
			client: spotify.New(httpClient),
			ctx:    ctx,
		}
		done := runCmd(np.pollState())

		// Cancel only once the request is in flight on the server.
		waitEntered(t, entered)
		cancel()

		// No fake time passes between cancel and the check, so the per-op
		// 10s timeout cannot have fired: only the cascade can end the call.
		msg := receiveNow(t, done, "pollState")
		psm, ok := msg.(playerStateMsg)
		if !ok {
			t.Fatalf("expected playerStateMsg, got %T", msg)
		}
		if !errors.Is(psm.err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", psm.err)
		}
	})
}

// While the spotify client is in a rate-limit cooldown, pollState must
// skip the API call (no network hit, no log spam) and pollInterval must
// extend the next tick past the deadline. Regression coverage for the
// post-sleep / network-change 429 storm.
func TestPollState_SkipsCallDuringRateLimitCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("Too many requests"))
		}))

		httpClient := &http.Client{Transport: &testutil.RewriteTransport{
			Base:   srv.Client().Transport,
			Target: srv.URL,
		}}
		client := spotify.New(httpClient)
		np := &nowPlayingModel{client: client, ctx: t.Context()}

		// First poll: arms the cooldown by hitting the 429.
		msg := np.pollState()()
		if _, ok := msg.(playerStateMsg); !ok {
			t.Fatalf("first poll: expected playerStateMsg, got %T", msg)
		}
		before := hits.Load()
		if before == 0 {
			t.Fatal("first poll: did not reach test server")
		}
		if !client.IsRateLimited() {
			t.Fatal("first poll: cooldown should be armed after 429")
		}

		// Second poll: must skip the network call entirely.
		msg2 := np.pollState()()
		psm, ok := msg2.(playerStateMsg)
		if !ok {
			t.Fatalf("second poll: expected playerStateMsg, got %T", msg2)
		}
		if !psm.skipped {
			t.Fatal("second poll: expected skipped=true during cooldown")
		}
		if hits.Load() != before {
			t.Errorf("second poll hit the network during cooldown (hits %d → %d)", before, hits.Load())
		}

		// pollInterval must reflect the cooldown so the next tick lands past it,
		// not the default 10s.
		if got := np.pollInterval(); got < 10*time.Second {
			t.Errorf("pollInterval during cooldown should be >= 10s, got %v", got)
		}
	})
}

// advanceProgress must only report a crossing on the *first* tick that
// pushes progressMs to durationMs. Once clamped, subsequent ticks stay
// past the end — without the "first crossing" guard, handleProgressTick
// would emit a fresh pollState every second, racing the regular tick's
// pollState at cooldown expiry and doubling API calls during a stall.
func TestAdvanceProgress_ReportsOnlyFirstEndCrossing(t *testing.T) {
	np := &nowPlayingModel{
		playing:    true,
		hasTrack:   true,
		durationMs: 3000,
	}

	np.progressMs = 500
	if np.advanceProgress() {
		t.Error("mid-track tick should not report end crossing")
	}
	if np.progressMs != 1500 {
		t.Errorf("progressMs after mid-track tick: got %d, want 1500", np.progressMs)
	}

	np.progressMs = 2500
	if !np.advanceProgress() {
		t.Error("tick that crosses end should report true")
	}
	if np.progressMs != np.durationMs {
		t.Errorf("progressMs not clamped: got %d, want %d", np.progressMs, np.durationMs)
	}

	for i := range 3 {
		if np.advanceProgress() {
			t.Errorf("post-end tick %d re-reported crossing", i)
		}
	}
}

// Paused / no-track states must never trigger a crossing — a poll would
// be pointless without a playing track.
func TestAdvanceProgress_QuietWhenPausedOrIdle(t *testing.T) {
	np := &nowPlayingModel{
		playing:    false,
		hasTrack:   true,
		progressMs: 2500,
		durationMs: 3000,
	}
	if np.advanceProgress() {
		t.Error("paused: should not report crossing")
	}
	if np.progressMs != 2500 {
		t.Errorf("paused: progressMs should not advance; got %d", np.progressMs)
	}

	np.playing = true
	np.hasTrack = false
	if np.advanceProgress() {
		t.Error("no track: should not report crossing")
	}
}

// Symmetrical coverage for view-level fetches: cancelling the root ctx
// must abort a playlist/track/etc fetch too. Uses playlistView as the
// representative since all lazy views share the same fetch shape.
func TestPlaylistFetch_RootContextCancelCascades(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, entered := newBlockingServer(t)

		httpClient := &http.Client{Transport: &testutil.RewriteTransport{
			Base:   srv.Client().Transport,
			Target: srv.URL,
		}}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		pv := newPlaylistView(ctx, spotify.New(httpClient), 80, 20, false)
		done := runCmd(pv.fetchMore())

		waitEntered(t, entered)
		cancel()

		msg := receiveNow(t, done, "playlist fetch")
		plm, ok := msg.(pageLoadedMsg)
		if !ok {
			t.Fatalf("expected pageLoadedMsg, got %T", msg)
		}
		if !errors.Is(plm.err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", plm.err)
		}
	})
}

// newBlockingServer returns an in-memory server whose handler signals on
// entered once a request arrives and then holds it open until the
// request's context is cancelled, so tests can cancel exactly while a call
// is in flight. The server shuts down with the test.
func newBlockingServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	entered := make(chan struct{}, 1)
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	return srv, entered
}

// waitEntered returns once the blocking server holds a request. Inside a
// bubble, synctest.Wait parks the caller until the client goroutine waits
// on its response and the handler on its context, so a request that never
// arrived shows up as an empty channel rather than a timeout.
func waitEntered(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	synctest.Wait()
	select {
	case <-entered:
	default:
		t.Fatal("request never reached the server")
	}
}

// A poll that started earlier but finished later must not roll the UI back
// to the state it saw.
func TestHandlePlayerState_DropsReplyOlderThanApplied(t *testing.T) {
	np := newTestNowPlaying(t)
	np.ctx = t.Context()
	np.pollState() // seq 1: slow poll, still sees the old track
	np.pollState() // seq 2: fast poll after "next"

	np.handlePlayerState(playerStateMsg{seq: 2, state: &spotify.PlayerState{TrackURI: "spotify:track:new", TrackName: "New", DurationMs: 1000}})
	np.handlePlayerState(playerStateMsg{seq: 1, state: &spotify.PlayerState{TrackURI: "spotify:track:old", TrackName: "Old", DurationMs: 1000}})

	if np.trackURI != "spotify:track:new" {
		t.Errorf("trackURI = %q; the older poll's reply was applied over the newer one", np.trackURI)
	}
}
