package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/lounge/tuify/internal/testutil"
)

// newTestClient creates a Client backed by a test HTTP server. Goes
// through New so the rate-limit gate is wired up the same way as in
// production. The handler receives all requests. The returned cleanup
// function must be deferred. Shared with every other *_test.go in the
// package.
func newTestClient(handler http.HandlerFunc) (*Client, func()) {
	srv := httptest.NewServer(handler)
	transport := &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}
	c := New(nil, &http.Client{Transport: transport})
	return c, srv.Close
}

func TestFetchUserID(t *testing.T) {
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/me") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "testuser123"})
	})
	defer cleanup()

	if err := c.FetchUserID(context.Background()); err != nil {
		t.Fatalf("FetchUserID: %v", err)
	}
	if c.userID != "testuser123" {
		t.Errorf("userID: got %q, want %q", c.userID, "testuser123")
	}
}

func TestDoWithRetry_429(t *testing.T) {
	var attempts atomic.Int32

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	})
	defer cleanup()

	body, status, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
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
}

func TestDoWithRetry_429_ExhaustedRetries(t *testing.T) {
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	})
	defer cleanup()

	_, status, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", status)
	}
}

func TestDoWithRetry_429_LongRetryAfter(t *testing.T) {
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	})
	defer cleanup()

	_, _, err := c.doWithRetry(context.Background(), "https://api.spotify.com/v1/test")
	if err == nil {
		t.Fatal("expected error for long retry-after")
	}
}

// Once the rateLimitTransport has armed a cooldown, subsequent doWithRetry
// calls must short-circuit at the transport (no network) and surface a
// structured *APIError with status 429 — not a wrapped url.Error.
func TestDoWithRetry_TransportShortCircuitTranslatesToAPIError(t *testing.T) {
	var hits atomic.Int32
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer cleanup()

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
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	})
	defer cleanup()

	var result struct{}
	err := c.apiGet(context.Background(), "https://api.spotify.com/v1/test", &result)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestApiGet_InvalidJSON(t *testing.T) {
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	})
	defer cleanup()

	var result struct{ Name string }
	err := c.apiGet(context.Background(), "https://api.spotify.com/v1/test", &result)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTruncateForLog_ShortInput(t *testing.T) {
	in := []byte("hello")
	out := truncateForLog(in)
	if string(out) != "hello" {
		t.Errorf("short input should be returned unchanged, got %q", out)
	}
}

func TestTruncateForLog_LongASCII(t *testing.T) {
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
