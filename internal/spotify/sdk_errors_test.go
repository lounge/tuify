package spotify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sp "github.com/zmb3/spotify/v2"
)

// sdkCalls lists every Client method that goes through the zmb3 SDK, with
// the op name its failures must report. Stop pauses first, so a failure
// there reports the pause op.
var sdkCalls = []struct {
	name string
	op   string
	call func(context.Context, *Client) error
}{
	{"Play", opPlay, func(ctx context.Context, c *Client) error { return c.Play(ctx, "spotify:track:1", "", "dev") }},
	{"PlayContext", opPlay, func(ctx context.Context, c *Client) error {
		return c.Play(ctx, "spotify:track:1", "spotify:playlist:1", "dev")
	}},
	{"PlayQueue", opPlay, func(ctx context.Context, c *Client) error {
		return c.PlayQueue(ctx, []string{"spotify:track:1"}, "dev")
	}},
	{"Resume", opPlay, func(ctx context.Context, c *Client) error { return c.Resume(ctx, "dev") }},
	{"Pause", opPause, func(ctx context.Context, c *Client) error { return c.Pause(ctx, "dev") }},
	{"Stop", opPause, func(ctx context.Context, c *Client) error { return c.Stop(ctx, "dev") }},
	{"Next", opNext, func(ctx context.Context, c *Client) error { return c.Next(ctx, "dev") }},
	{"Previous", opPrevious, func(ctx context.Context, c *Client) error { return c.Previous(ctx, "dev") }},
	{"Shuffle", opShuffle, func(ctx context.Context, c *Client) error { return c.Shuffle(ctx, true, "dev") }},
	{"Seek", opSeek, func(ctx context.Context, c *Client) error { return c.Seek(ctx, 1000, "dev") }},
	{"GetDevices", opDevices, func(ctx context.Context, c *Client) error {
		_, err := c.GetDevices(ctx)
		return err
	}},
	{"FindDevice", opDevices, func(ctx context.Context, c *Client) error {
		_, _, _, err := c.FindDevice(ctx, false)
		return err
	}},
	{"TransferPlayback", opTransfer, func(ctx context.Context, c *Client) error {
		return c.TransferPlayback(ctx, "dev", true)
	}},
}

func TestSDKMethods_ErrorResponseIsAPIError(t *testing.T) {
	t.Parallel()

	const msg = "Player command failed: Restriction violated"
	for _, tc := range sdkCalls {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":{"status":403,"message":"` + msg + `"}}`))
			})

			err := tc.call(context.Background(), c)
			apiErr, ok := errors.AsType[*APIError](err)
			if !ok {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.Status != http.StatusForbidden {
				t.Errorf("Status: got %d, want 403", apiErr.Status)
			}
			if string(apiErr.Body) != msg {
				t.Errorf("Body: got %q, want %q", apiErr.Body, msg)
			}
			if apiErr.Endpoint != tc.op {
				t.Errorf("Endpoint: got %q, want %q", apiErr.Endpoint, tc.op)
			}
			// The SDK's own error type must not leak through Unwrap, or
			// callers could couple to zmb3 via errors.As.
			if _, ok := errors.AsType[sp.Error](err); ok {
				t.Error("sp.Error reachable via errors.As; SDK type leaks through APIError")
			}
		})
	}
}

func TestSDKMethods_CooldownIsAPIError429(t *testing.T) {
	t.Parallel()

	for _, tc := range sdkCalls {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusNoContent)
			})
			deadline := time.Now().Add(time.Minute)
			c.rl.setUntil(deadline)

			err := tc.call(context.Background(), c)
			apiErr, ok := errors.AsType[*APIError](err)
			if !ok {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.Status != http.StatusTooManyRequests {
				t.Errorf("Status: got %d, want 429", apiErr.Status)
			}
			rle, ok := errors.AsType[*RateLimitedError](err)
			if !ok {
				t.Fatal("*RateLimitedError not reachable via errors.As")
			}
			if !rle.Until.Equal(time.Unix(0, deadline.UnixNano())) {
				t.Errorf("Until: got %v, want %v", rle.Until, deadline)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("request reached the server during cooldown (%d hits)", n)
			}
		})
	}
}

func TestWrapSDKErr_PassThrough(t *testing.T) {
	t.Parallel()

	if got := wrapSDKErr(nil, opPlay); got != nil {
		t.Errorf("nil: got %v, want nil", got)
	}
	if got := wrapSDKErr(context.Canceled, opPlay); !errors.Is(got, context.Canceled) {
		t.Errorf("context.Canceled: got %v", got)
	}
	if _, ok := errors.AsType[*APIError](wrapSDKErr(context.DeadlineExceeded, opPlay)); ok {
		t.Error("context.DeadlineExceeded must not become an *APIError")
	}
	orig := &APIError{Status: http.StatusNotFound, Endpoint: "x"}
	if got := wrapSDKErr(orig, opPlay); got != orig {
		t.Errorf("existing *APIError: got %v, want it returned unchanged", got)
	}
	plain := errors.New("spotify: HTTP 404: Not Found (body empty)")
	if got := wrapSDKErr(plain, opPlay); got != plain || strings.Contains(got.Error(), "API") {
		t.Errorf("undecodable SDK error: got %v, want unchanged", got)
	}
}
