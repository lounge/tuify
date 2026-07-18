package spotify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestRateLimitTransport_NoRetryAfterTriggersCooldown reproduces the field
// scenario: Spotify returns 429 with no Retry-After (after sleep/network
// changes pile up requests). The transport must lock out subsequent calls
// instead of letting the polling loop keep hammering the API.
func TestRateLimitTransport_NoRetryAfterTriggersCooldown(t *testing.T) {
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
	var rle *RateLimitedError
	if !errors.As(err, &rle) {
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

// TestRateLimitTransport_ExpiredCooldownAllowsCalls verifies the gate
// re-opens once the deadline passes.
func TestRateLimitTransport_ExpiredCooldownAllowsCalls(t *testing.T) {
	rl := newRateLimitTransport(http.DefaultTransport)
	rl.until.Store(time.Now().Add(-time.Second).UnixNano())

	if got := rl.wait(); got != 0 {
		t.Errorf("expired deadline should report wait=0, got %v", got)
	}
}

// TestRateLimitTransport_ConsecutiveCooldownEscalates reproduces the
// field scenario where Spotify keeps returning 429 across a long period:
// each successive 429 without an intervening success must arm a longer
// cooldown than the previous one, otherwise polling gets stuck in a
// fixed-interval retry loop for hours.
func TestRateLimitTransport_ConsecutiveCooldownEscalates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rl := newRateLimitTransport(srv.Client().Transport)
	client := &http.Client{Transport: rl}

	var prev time.Duration
	for i := 1; i <= 4; i++ {
		// Force the deadline to expire so this iteration reaches the network
		// and arms a fresh cooldown — simulating "cooldown expired, poller
		// retried, got another 429".
		rl.until.Store(0)
		req, _ := http.NewRequest("GET", srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("iter %d: unexpected err: %v", i, err)
		}
		resp.Body.Close()

		got := rl.wait()
		if got <= 0 {
			t.Fatalf("iter %d: cooldown not armed", i)
		}
		if i > 1 && got <= prev {
			t.Errorf("iter %d: cooldown %v did not exceed previous %v", i, got, prev)
		}
		prev = got
	}
}

// TestRateLimitTransport_ResetsConsecutiveOnSuccess verifies the streak
// resets after any non-429 response, so the *next* 429 arms a base-level
// cooldown rather than resuming the escalated one from an earlier storm.
func TestRateLimitTransport_ResetsConsecutiveOnSuccess(t *testing.T) {
	var succeed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if succeed.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rl := newRateLimitTransport(srv.Client().Transport)
	client := &http.Client{Transport: rl}

	// Escalate the streak with two 429s.
	for i := 0; i < 2; i++ {
		rl.until.Store(0)
		req, _ := http.NewRequest("GET", srv.URL, nil)
		resp, _ := client.Do(req)
		resp.Body.Close()
	}
	escalated := rl.wait()
	if escalated <= 0 {
		t.Fatal("cooldown not armed after two 429s")
	}

	// One 2xx response — must reset the streak.
	succeed.Store(true)
	rl.until.Store(0)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("success call: %v", err)
	}
	resp.Body.Close()
	if got := rl.consecutive.Load(); got != 0 {
		t.Errorf("consecutive not reset after 2xx: got %d, want 0", got)
	}

	// Next 429 should arm a base-level cooldown, not the escalated one.
	succeed.Store(false)
	rl.until.Store(0)
	req2, _ := http.NewRequest("GET", srv.URL, nil)
	resp2, _ := client.Do(req2)
	resp2.Body.Close()

	after := rl.wait()
	if after >= escalated {
		t.Errorf("post-reset cooldown %v not shorter than escalated %v", after, escalated)
	}
}
