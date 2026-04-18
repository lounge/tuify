package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// Cancelling the root context must cascade: any in-flight pollState()
// command sees the cancellation and returns playerStateMsg with a
// context.Canceled (or DeadlineExceeded) error instead of waiting for
// the per-op timeout. Proves the ctx threading from bootstrap.Run is
// wired up correctly for the now-playing poll path.
func TestPollState_RootContextCancelCascadesToHTTPCall(t *testing.T) {
	// Blocking server: holds the request open until its own request
	// context is cancelled. Our cancel() must trigger that.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	httpClient := &http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	np := &nowPlayingModel{
		client: spotify.New(nil, httpClient),
		ctx:    ctx,
	}
	cmd := np.pollState()

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// Give the goroutine time to enter the HTTP call, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case msg := <-done:
		psm, ok := msg.(playerStateMsg)
		if !ok {
			t.Fatalf("expected playerStateMsg, got %T", msg)
		}
		if psm.err == nil {
			t.Fatal("expected error from canceled pollState, got nil")
		}
		// Either context.Canceled (our cancel won the race) or
		// context.DeadlineExceeded (the per-op 10s timeout wraps ctx);
		// both prove cancellation propagated.
		if !errors.Is(psm.err, context.Canceled) &&
			!errors.Is(psm.err, context.DeadlineExceeded) {
			t.Errorf("expected ctx.Canceled/DeadlineExceeded, got %v", psm.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pollState did not return after root ctx cancel — cascade broken")
	}
}

// Symmetrical coverage for view-level fetches: cancelling the root ctx
// must abort a playlist/track/etc fetch too. Uses playlistView as the
// representative since all lazy views share the same fetch shape.
func TestPlaylistFetch_RootContextCancelCascades(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	httpClient := &http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pv := newPlaylistView(ctx, spotify.New(nil, httpClient), 80, 20, false)
	cmd := pv.fetchMore()

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case msg := <-done:
		plm, ok := msg.(playlistsLoadedMsg)
		if !ok {
			t.Fatalf("expected playlistsLoadedMsg, got %T", msg)
		}
		if plm.err == nil {
			t.Fatal("expected error from canceled fetch, got nil")
		}
		if !errors.Is(plm.err, context.Canceled) &&
			!errors.Is(plm.err, context.DeadlineExceeded) {
			t.Errorf("expected ctx.Canceled/DeadlineExceeded, got %v", plm.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("playlist fetch did not return after root ctx cancel")
	}
}
