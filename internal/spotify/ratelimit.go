package spotify

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// RateLimitedError is returned by the http transport when a request is
// short-circuited because the client is in a rate-limit cooldown window.
// The current call returns immediately without hitting the network.
type RateLimitedError struct {
	Until time.Time
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited until %s", e.Until.Format("15:04:05"))
}

const (
	// rateLimitMinBackoff is the cooldown applied when Spotify returns 429
	// without a usable Retry-After header. After sleep/network changes the
	// header is often missing or zero, and at that point we want a real
	// pause — not another request 10 seconds later.
	rateLimitMinBackoff = 30 * time.Second
	// rateLimitMaxBackoff caps the cooldown so a bogus Retry-After can't
	// wedge the client for an unreasonable time.
	rateLimitMaxBackoff = 5 * time.Minute
	// rateLimitInlineRetryThreshold matches doWithRetry's retry budget:
	// 429s with Retry-After at or below this are handled by the inline
	// retry loop, so we leave the global cooldown unset for those.
	rateLimitInlineRetryThreshold = 10
)

// rateLimitTransport gates outgoing requests on a shared cooldown deadline.
// On 429 with a missing or large Retry-After it sets the deadline; while a
// deadline is in the future, RoundTrip returns *RateLimitedError without
// performing the network call. Both the SDK and our raw HTTP path share
// the same transport, so a single 429 throttles all subsequent calls.
type rateLimitTransport struct {
	base  http.RoundTripper
	until atomic.Int64 // unix nanos; 0 means not rate limited
}

func newRateLimitTransport(base http.RoundTripper) *rateLimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &rateLimitTransport{base: base}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if u := t.until.Load(); u > 0 {
		deadline := time.Unix(0, u)
		if time.Now().Before(deadline) {
			return nil, &RateLimitedError{Until: deadline}
		}
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return resp, err
	}
	hasRetryAfter := false
	wait := 0
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, perr := strconv.Atoi(s); perr == nil {
			hasRetryAfter = true
			wait = n
		}
	}
	if hasRetryAfter && wait <= rateLimitInlineRetryThreshold {
		// Brief throttle with a known short backoff — the caller's inline
		// retry loop handles this without locking out everything else.
		return resp, nil
	}
	cooldown := time.Duration(wait) * time.Second
	if cooldown < rateLimitMinBackoff {
		cooldown = rateLimitMinBackoff
	}
	if cooldown > rateLimitMaxBackoff {
		cooldown = rateLimitMaxBackoff
	}
	t.setUntil(time.Now().Add(cooldown))
	return resp, nil
}

func (t *rateLimitTransport) setUntil(deadline time.Time) {
	newVal := deadline.UnixNano()
	for {
		cur := t.until.Load()
		if newVal <= cur {
			return
		}
		if t.until.CompareAndSwap(cur, newVal) {
			log.Printf("[ratelimit] cooldown set: pausing API calls until %s", deadline.Format("15:04:05"))
			return
		}
	}
}

// wait returns the remaining cooldown duration, or 0 if not rate limited.
func (t *rateLimitTransport) wait() time.Duration {
	u := t.until.Load()
	if u == 0 {
		return 0
	}
	remaining := time.Until(time.Unix(0, u))
	if remaining <= 0 {
		return 0
	}
	return remaining
}
