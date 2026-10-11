package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestRateLimitTransport_NoRetryAfterTriggersCooldown reproduces the field
// scenario: Spotify returns 429 with no Retry-After (after sleep/network
// changes pile up requests). The transport must lock out subsequent calls
// instead of letting the polling loop keep hammering the API.
func TestRateLimitTransport_NoRetryAfterTriggersCooldown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("Too many requests"))
		}))

		rl := newRateLimitTransport(srv.Client().Transport)
		client := &http.Client{Transport: rl}

		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("first call: unexpected err: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("first call: status got %d want 429", resp.StatusCode)
		}
		if hits.Load() != 1 {
			t.Fatalf("first call: hits got %d want 1", hits.Load())
		}

		req2, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp2, err := client.Do(req2)
		if resp2 != nil {
			resp2.Body.Close()
		}
		if err == nil {
			t.Fatal("second call: expected RateLimitedError, got nil")
		}
		if _, ok := errors.AsType[*RateLimitedError](err); !ok {
			t.Fatalf("second call: expected *RateLimitedError, got %T: %v", err, err)
		}
		if hits.Load() != 1 {
			t.Errorf("second call hit the network (hits=%d); cooldown not enforced", hits.Load())
		}
		if got := rl.wait(); got <= 0 || got > rateLimitMaxBackoff {
			t.Errorf("wait(): got %v, expected in (0, %v]", got, rateLimitMaxBackoff)
		}
	})
}

// TestRateLimitTransport_ShortRetryAfter: a 429 with a small Retry-After
// leaves the cooldown unset for a request whose caller retries inline
// (doWithRetry), so it doesn't lock out everything else. Nothing retries
// an SDK request, whose next call used to go straight back to Spotify:
// that one holds every call for exactly the Retry-After, without
// escalating the streak.
func TestRateLimitTransport_ShortRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctx      func(context.Context) context.Context
		wantWait time.Duration
	}{
		{"inline retry passes through", withInlineRetry, 0},
		{"no inline retry waits Retry-After", func(ctx context.Context) context.Context { return ctx }, 2 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					w.Header().Set("Retry-After", "2")
					w.WriteHeader(http.StatusTooManyRequests)
				}))

				rl := newRateLimitTransport(srv.Client().Transport)
				client := &http.Client{Transport: rl}

				req, _ := http.NewRequestWithContext(tc.ctx(t.Context()), http.MethodGet, srv.URL, nil)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				resp.Body.Close()

				if got := rl.wait(); got != tc.wantWait {
					t.Errorf("cooldown = %v, want %v", got, tc.wantWait)
				}
				if got := rl.consecutive.Load(); got != 0 {
					t.Errorf("consecutive = %d, want 0: a short, known backoff is no escalation", got)
				}
				if tc.wantWait == 0 {
					return
				}
				req, _ = http.NewRequestWithContext(tc.ctx(t.Context()), http.MethodGet, srv.URL, nil)
				resp, err = client.Do(req)
				if resp != nil {
					resp.Body.Close()
				}
				if _, ok := errors.AsType[*RateLimitedError](err); !ok {
					t.Errorf("call inside the Retry-After: err = %v, want *RateLimitedError", err)
				}
				if got := hits.Load(); got != 1 {
					t.Errorf("server saw %d requests, want 1", got)
				}
			})
		})
	}
}

// TestRateLimitTransport_LargeRetryAfterCapped verifies a malicious or
// out-of-range Retry-After is capped to rateLimitMaxBackoff.
func TestRateLimitTransport_LargeRetryAfterCapped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "99999")
			w.WriteHeader(http.StatusTooManyRequests)
		}))

		rl := newRateLimitTransport(srv.Client().Transport)
		client := &http.Client{Transport: rl}

		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		got := rl.wait()
		if got > rateLimitMaxBackoff {
			t.Errorf("wait %v exceeds cap %v", got, rateLimitMaxBackoff)
		}
		if got <= 0 {
			t.Errorf("expected cooldown to be set, got %v", got)
		}
	})
}

// stubTransport answers every request in memory with the status status()
// returns, counting hits. The escalation tests below exercise only the
// transport under test and the fake clock, so a stub is simpler than a
// server there.
type stubTransport struct {
	status func() int
	header http.Header // optional response headers, e.g. Retry-After
	hits   atomic.Int32
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.hits.Add(1)
	header := s.header
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: s.status(),
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func always(status int) func() int { return func() int { return status } }

// get sends one GET through rl and closes the body. It returns the error
// so callers can tell a gated call (*RateLimitedError) from a real one.
func get(t *testing.T, rl *rateLimitTransport) error {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "https://api.spotify.com/v1/me/player", nil)
	resp, err := rl.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

// TestRateLimitTransport_NonPositiveRetryAfterArmsCooldown: a Retry-After
// of zero or less is no backoff at all, so it must be treated like a
// missing header and arm the base cooldown instead of passing through to
// an immediate inline retry.
func TestRateLimitTransport_NonPositiveRetryAfterArmsCooldown(t *testing.T) {
	t.Parallel()

	for _, retryAfter := range []string{"0", "-5"} {
		t.Run("Retry-After "+retryAfter, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				stub := &stubTransport{
					status: always(http.StatusTooManyRequests),
					header: http.Header{"Retry-After": {retryAfter}},
				}
				rl := newRateLimitTransport(stub)

				if err := get(t, rl); err != nil {
					t.Fatalf("first call: %v", err)
				}
				if got := rl.wait(); got != rateLimitMinBackoff {
					t.Errorf("cooldown = %v, want %v", got, rateLimitMinBackoff)
				}
				if err := get(t, rl); err == nil || stub.hits.Load() != 1 {
					t.Errorf("second call reached the network (hits=%d, err=%v); cooldown not enforced", stub.hits.Load(), err)
				}
			})
		})
	}
}

// TestRateLimitTransport_ExpiredCooldownAllowsCalls verifies the gate
// holds calls back until the deadline and re-opens once it passes.
func TestRateLimitTransport_ExpiredCooldownAllowsCalls(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		stub := &stubTransport{status: always(http.StatusTooManyRequests)}
		rl := newRateLimitTransport(stub)

		get(t, rl) // arms the 30s cooldown
		time.Sleep(rateLimitMinBackoff - time.Second)
		if err := get(t, rl); err == nil || stub.hits.Load() != 1 {
			t.Fatalf("call before the deadline reached the network (hits=%d, err=%v)", stub.hits.Load(), err)
		}

		time.Sleep(time.Second)
		if got := rl.wait(); got != 0 {
			t.Errorf("expired deadline should report wait=0, got %v", got)
		}
		stub.status = always(http.StatusOK)
		if err := get(t, rl); err != nil || stub.hits.Load() != 2 {
			t.Errorf("call after the deadline should reach the network (hits=%d, err=%v)", stub.hits.Load(), err)
		}
	})
}

// TestRateLimitTransport_ConsecutiveCooldownEscalates reproduces the
// field scenario where Spotify keeps returning 429 across a long period:
// each successive 429 without an intervening success must arm a longer
// cooldown than the previous one, otherwise polling gets stuck in a
// fixed-interval retry loop for hours. Each iteration waits out the
// cooldown on the fake clock, as the poller would, before retrying.
func TestRateLimitTransport_ConsecutiveCooldownEscalates(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rl := newRateLimitTransport(&stubTransport{status: always(http.StatusTooManyRequests)})

		want := rateLimitMinBackoff
		for i := 1; i <= 8; i++ {
			if err := get(t, rl); err != nil {
				t.Fatalf("iter %d: call gated after the cooldown expired: %v", i, err)
			}
			if got := rl.wait(); got != want {
				t.Fatalf("iter %d: cooldown %v, want %v", i, got, want)
			}
			time.Sleep(rl.wait())
			want = min(want*2, rateLimitMaxBackoff)
		}
	})
}

// TestRateLimitTransport_ResetsConsecutiveOnSuccess verifies the streak
// resets after any non-429 response, so the *next* 429 arms a base-level
// cooldown rather than resuming the escalated one from an earlier storm.
func TestRateLimitTransport_ResetsConsecutiveOnSuccess(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		status := http.StatusTooManyRequests
		rl := newRateLimitTransport(&stubTransport{status: func() int { return status }})

		// Escalate the streak with two 429s.
		for range 2 {
			get(t, rl)
			time.Sleep(rl.wait())
		}

		// One 2xx response — must reset the streak.
		status = http.StatusOK
		if err := get(t, rl); err != nil {
			t.Fatalf("success call: %v", err)
		}
		if got := rl.consecutive.Load(); got != 0 {
			t.Errorf("consecutive not reset after 2xx: got %d, want 0", got)
		}

		// Next 429 should arm the base cooldown, not the escalated one.
		status = http.StatusTooManyRequests
		get(t, rl)
		if got := rl.wait(); got != rateLimitMinBackoff {
			t.Errorf("post-reset cooldown %v, want base %v", got, rateLimitMinBackoff)
		}
	})
}

// barrierTransport holds every request until n have arrived, then answers
// them all with 429, so their responses race back in one burst.
type barrierTransport struct {
	n       int32
	arrived atomic.Int32
	release chan struct{}
}

func (b *barrierTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.arrived.Add(1) == b.n {
		close(b.release)
	}
	<-b.release
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
}

// TestRateLimitTransport_ParallelBurstArmsBaseCooldown: requests that were
// in flight together and all got 429 are one throttle, not three
// consecutive ones. Counting each response escalated the first cooldown
// to 2 minutes.
func TestRateLimitTransport_ParallelBurstArmsBaseCooldown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const n = 3
		rl := newRateLimitTransport(&barrierTransport{n: n, release: make(chan struct{})})

		var wg sync.WaitGroup
		for range n {
			wg.Go(func() { get(t, rl) })
		}
		wg.Wait()

		if got := rl.wait(); got != rateLimitMinBackoff {
			t.Errorf("cooldown after a burst of %d parallel 429s = %v, want %v", n, got, rateLimitMinBackoff)
		}
		if got := rl.consecutive.Load(); got != 1 {
			t.Errorf("consecutive = %d, want 1", got)
		}
	})
}
