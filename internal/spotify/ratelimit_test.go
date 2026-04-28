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
