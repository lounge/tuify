package spotify

import (
	"errors"
	"net/http"
	"net/http/httptest"
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

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("Too many requests"))
	}))
	defer srv.Close()

	rl := newRateLimitTransport(srv.Client().Transport)
	client := &http.Client{Transport: rl}

	req, _ := http.NewRequest("GET", srv.URL, nil)
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

	req2, _ := http.NewRequest("GET", srv.URL, nil)
	_, err = client.Do(req2)
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
}

// TestRateLimitTransport_ShortRetryAfterPassesThrough verifies the inline
// retry path is preserved: a 429 with a small Retry-After lets doWithRetry
// retry without locking out everything else.
func TestRateLimitTransport_ShortRetryAfterPassesThrough(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rl := newRateLimitTransport(srv.Client().Transport)
	client := &http.Client{Transport: rl}

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	resp.Body.Close()

	if got := rl.wait(); got != 0 {
		t.Errorf("short Retry-After should not arm cooldown, got wait=%v", got)
	}
}

// TestRateLimitTransport_LargeRetryAfterCapped verifies a malicious or
// out-of-range Retry-After is capped to rateLimitMaxBackoff.
func TestRateLimitTransport_LargeRetryAfterCapped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "99999")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rl := newRateLimitTransport(srv.Client().Transport)
	client := &http.Client{Transport: rl}

	req, _ := http.NewRequest("GET", srv.URL, nil)
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
}

// stubTransport answers every request in memory with the status status()
// returns, counting hits. Unlike httptest it does no network I/O, so it
// works inside a synctest bubble where only the fake clock advances.
type stubTransport struct {
	status func() int
	hits   atomic.Int32
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.hits.Add(1)
	return &http.Response{
		StatusCode: s.status(),
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func always(status int) func() int { return func() int { return status } }

// get sends one GET through rl and closes the body. It returns the error
// so callers can tell a gated call (*RateLimitedError) from a real one.
func get(t *testing.T, rl *rateLimitTransport) error {
	t.Helper()
	req, _ := http.NewRequest("GET", "https://api.spotify.com/v1/me/player", nil)
	resp, err := rl.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
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
