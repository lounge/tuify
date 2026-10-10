package spotify

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/lounge/tuify/internal/testutil"
)

// newTestClient creates a Client backed by a test HTTP server. Goes
// through New so the rate-limit gate is wired up the same way as in
// production. The handler receives all requests; the server closes when
// the test ends. Shared with every other *_test.go in the package.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(&http.Client{Transport: &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}})
}

// newBubbleClient is newTestClient on an in-memory server, for tests that
// run inside synctest.Test so Retry-After waits elapse on the fake clock.
func newBubbleClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTestServer(t, handler)
	return New(&http.Client{Transport: &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}})
}

func TestFetchUserID(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/me") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.MarshalWrite(w, map[string]string{"id": "testuser123"})
	})

	if err := c.FetchUserID(context.Background()); err != nil {
		t.Fatalf("FetchUserID: %v", err)
	}
	if c.userID != "testuser123" {
		t.Errorf("userID: got %q, want %q", c.userID, "testuser123")
	}
}

func TestDoWithRetry_429(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32

		c := newBubbleClient(t, func(w http.ResponseWriter, r *http.Request) {
			n := attempts.Add(1)
			if n <= 2 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte("rate limited"))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok": true}`))
		})

		body, status, err := c.doWithRetry(t.Context(), "https://api.spotify.com/v1/test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status != http.StatusOK {
			t.Errorf("status: got %d, want 200", status)
		}
		if string(body) != `{"ok": true}` {
			t.Errorf("body: got %q", string(body))
		}
		if attempts.Load() != 3 {
			t.Errorf("attempts: got %d, want 3", attempts.Load())
		}
	})
}

func TestDoWithRetry_429_ExhaustedRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var hits atomic.Int32
		c := newBubbleClient(t, func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
		})

		_, status, err := c.doWithRetry(t.Context(), "https://api.spotify.com/v1/test")
		if err == nil {
			t.Fatal("expected error after exhausted retries")
		}
		if status != http.StatusTooManyRequests {
			t.Errorf("status: got %d, want 429", status)
		}
		apiErr, ok := errors.AsType[*APIError](err)
		if !ok {
			t.Fatalf("expected *APIError, got %T: %v", err, err)
		}
		if apiErr.Status != http.StatusTooManyRequests {
			t.Errorf("APIError.Status: got %d, want 429", apiErr.Status)
		}

		// Three short throttles in a row for one request are a sustained
		// throttle: the cooldown is armed at the base level, the streak
		// counts it once, and the next call does not reach the network.
		if _, ok := errors.AsType[*RateLimitedError](err); !ok {
			t.Error("exhausted retries: *RateLimitedError not reachable, caller cannot see the deadline")
		}
		if got := c.RateLimitWait(); got != rateLimitMinBackoff {
			t.Errorf("cooldown after exhausted retries = %v, want %v", got, rateLimitMinBackoff)
		}
		if got := c.rl.consecutive.Load(); got != 1 {
			t.Errorf("consecutive = %d, want 1", got)
		}
		before := hits.Load()
		if _, _, err := c.doWithRetry(t.Context(), "https://api.spotify.com/v1/test"); err == nil {
			t.Fatal("call during the cooldown succeeded")
		}
		if hits.Load() != before {
			t.Error("call during the cooldown reached the network")
		}

		// A second exhausted storm after the cooldown escalates it.
		time.Sleep(c.RateLimitWait())
		c.doWithRetry(t.Context(), "https://api.spotify.com/v1/test")
		if got := c.RateLimitWait(); got != 2*rateLimitMinBackoff {
			t.Errorf("second storm cooldown = %v, want %v", got, 2*rateLimitMinBackoff)
		}
	})
}

// A single short Retry-After is still retried inline without arming the
// shared cooldown.
func TestDoWithRetry_429_SingleShortRetryAfterDoesNotArm(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		c := newBubbleClient(t, func(w http.ResponseWriter, r *http.Request) {
			if attempts.Add(1) == 1 {
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Write([]byte(`{}`))
		})
		if _, _, err := c.doWithRetry(t.Context(), "https://api.spotify.com/v1/test"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.IsRateLimited() {
			t.Error("one short Retry-After armed the cooldown")
		}
	})
}

// A 429 with "Retry-After: 0" used to pass the transport and then retry
// without waiting, so one throttle became three back-to-back requests. It
// must arm the cooldown like a missing header: one request, then a 429
// *APIError without touching the network again.
func TestDoWithRetry_429_ZeroRetryAfterArmsCooldown(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, status, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
	if status != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", status)
	}
	if _, ok := errors.AsType[*APIError](err); !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if n := attempts.Load(); n != 1 {
		t.Errorf("attempts: got %d, want 1", n)
	}
	if !c.IsRateLimited() {
		t.Error("Retry-After: 0 did not arm the cooldown")
	}
}

func TestDoWithRetry_429_LongRetryAfter(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	})

	_, _, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
	if err == nil {
		t.Fatal("expected error for long retry-after")
	}
}

// Once the rateLimitTransport has armed a cooldown, subsequent doWithRetry
// calls must short-circuit at the transport (no network) and surface a
// structured *APIError with status 429 — not a wrapped url.Error.
func TestDoWithRetry_TransportShortCircuitTranslatesToAPIError(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	// First call arms the cooldown.
	if _, _, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test"); err == nil {
		t.Fatal("first call: expected error from 429")
	}
	armed := hits.Load()

	// Second call must short-circuit at the transport.
	_, status, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
	if err == nil {
		t.Fatal("second call: expected error from cooldown short-circuit")
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status: got %d want 429", status)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError translation, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusTooManyRequests {
		t.Errorf("APIError.Status: got %d want 429", apiErr.Status)
	}
	if hits.Load() != armed {
		t.Errorf("second call hit the network (hits %d → %d); transport short-circuit failed", armed, hits.Load())
	}
}

func TestApiGet_NonOK(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})

	var result struct{}
	err := c.apiGet(context.Background(), "https://api.spotify.com/v1/test", &result)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// A gateway in front of the API answers with an HTML page. The raw REST
// path must keep the status and must not carry the page in its error.
func TestApiGet_HTMLErrorBodyKeepsStatus(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html><body><h1>502 Bad Gateway</h1></body></html>"))
	})
	var result struct{}
	err := c.apiGet(context.Background(), "https://api.spotify.com/v1/test", &result)
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("Status = %d, want 502", apiErr.Status)
	}
	if strings.Contains(err.Error(), "<html") {
		t.Errorf("error carries the HTML page: %v", err)
	}
}

func TestApiGet_InvalidJSON(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	})

	var result struct{ Name string }
	err := c.apiGet(context.Background(), "https://api.spotify.com/v1/test", &result)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTruncateForLog_ShortInput(t *testing.T) {
	t.Parallel()

	in := []byte("hello")
	out := truncateForLog(in)
	if string(out) != "hello" {
		t.Errorf("short input should be returned unchanged, got %q", out)
	}
}

func TestTruncateForLog_LongASCII(t *testing.T) {
	t.Parallel()

	in := make([]byte, 1000)
	for i := range in {
		in[i] = 'a'
	}
	out := truncateForLog(in)
	if !utf8.Valid(out) {
		t.Errorf("output is not valid UTF-8: %q", out)
	}
}

// TestTruncateForLog_MultibyteBoundary reproduces the class of bug where the
// cut fell in the middle of a multi-byte rune, yielding malformed bytes.
// Every prefix length must still produce valid UTF-8 output.
func TestTruncateForLog_MultibyteBoundary(t *testing.T) {
	t.Parallel()

	// Build a payload where the 500-byte cut lands mid-rune. "日" is 3 bytes.
	// Prefixing 499 ASCII bytes means position 500 is the 2nd byte of 日.
	in := make([]byte, 0, 1000)
	for range 499 {
		in = append(in, 'x')
	}
	for range 200 {
		in = append(in, []byte("日")...)
	}

	out := truncateForLog(in)
	if !utf8.Valid(out) {
		t.Errorf("truncated output has invalid UTF-8: %q", out)
	}
}
