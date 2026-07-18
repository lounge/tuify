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
	// wedge the client for an unreasonable time. Set high enough that a
	// prolonged Spotify throttle (13h+ observed in the field) can escalate
	// out of a fixed short-interval retry loop.
	rateLimitMaxBackoff = 1 * time.Hour
	// rateLimitInlineRetryThreshold matches doWithRetry's retry budget:
	// 429s with Retry-After at or below this are handled by the inline
	// retry loop, so we leave the global cooldown unset for those.
	rateLimitInlineRetryThreshold = 10
	// rateLimitMaxShift caps the exponential multiplier applied to the
	// base cooldown so consecutive-429 arithmetic can't overflow. 12 is
	// well beyond the point where the cooldown saturates at max.
	rateLimitMaxShift = 12
)

// rateLimitTransport gates outgoing requests on a shared cooldown deadline.
// On 429 with a missing or large Retry-After it sets the deadline; while a
// deadline is in the future, RoundTrip returns *RateLimitedError without
// performing the network call. Both the SDK and our raw HTTP path share
// the same transport, so a single 429 throttles all subsequent calls.
//
// Consecutive 429s (without an intervening non-429 response) apply an
// exponential multiplier to the cooldown, so a persistent throttle backs
// off instead of pinging Spotify at a fixed interval indefinitely. A
// non-429 response resets the counter.
type rateLimitTransport struct {
	base        http.RoundTripper
	until       atomic.Int64 // unix nanos; 0 means not rate limited
	consecutive atomic.Int32 // consecutive 429s since the last non-429
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
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		// Any non-429 response — success or otherwise — means Spotify isn't
		// currently throttling this client. Reset the streak so a future
		// 429 starts at the base cooldown instead of resuming a stale
		// escalation.
		t.consecutive.Store(0)
		return resp, nil
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
	// Exponential backoff: each consecutive 429 doubles the base cooldown.
	// Shift capped so overflow can't produce a negative Duration; the
	// result is clamped to rateLimitMaxBackoff regardless.
	n := t.consecutive.Add(1)
	shift := n - 1
	if shift > rateLimitMaxShift {
		shift = rateLimitMaxShift
	}
	cooldown *= 1 << shift
	if cooldown > rateLimitMaxBackoff || cooldown <= 0 {
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
			// gosec G706 (log injection) taint-tracks a numeric Retry-After
			// header through to this log line. deadline.Format with a
			// literal layout emits only digits and colons, so no external
			// content can reach the log.
			log.Printf("[ratelimit] cooldown set: pausing API calls until %s", deadline.Format("15:04:05")) //nolint:gosec
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
