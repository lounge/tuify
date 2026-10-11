package spotify

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
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
	// retry loop, so we leave the global cooldown unset for those. A
	// request with no such loop behind it (the SDK path) gets a cooldown
	// of exactly that Retry-After instead.
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
// the same transport, so a single 429 throttles all subsequent calls. A
// short Retry-After is left to the caller's inline retry when the request
// carries withInlineRetry; any other request sets the deadline to it.
//
// Consecutive 429s (without an intervening non-429 response) apply an
// exponential multiplier to the cooldown, so a persistent throttle backs
// off instead of pinging Spotify at a fixed interval indefinitely. A
// non-429 response resets the counter.
type rateLimitTransport struct {
	base        http.RoundTripper
	until       atomic.Int64 // unix nanos; 0 means not rate limited
	consecutive atomic.Int32 // consecutive 429s since the last non-429

	// armMu serializes arming a cooldown so that concurrent 429s from one
	// burst escalate the streak once, not once per response.
	armMu sync.Mutex
}

func newRateLimitTransport(base http.RoundTripper) *rateLimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &rateLimitTransport{base: base}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// sentUntil is the deadline in force when this request left. If it has
	// changed by the time a 429 comes back, a parallel request of the same
	// burst already armed the cooldown.
	sentUntil := t.until.Load()
	if sentUntil > 0 {
		deadline := time.Unix(0, sentUntil)
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
	// A Retry-After of zero or less is treated as missing: it carries no
	// usable backoff, and honouring it would let the caller's inline retry
	// loop fire its three attempts back to back.
	hasRetryAfter := false
	wait := 0
	if s := resp.Header.Get("Retry-After"); s != "" {
		if n, perr := strconv.Atoi(s); perr == nil && n > 0 {
			hasRetryAfter = true
			wait = n
		}
	}
	if hasRetryAfter && wait <= rateLimitInlineRetryThreshold {
		if retriesInline(req.Context()) {
			// Brief throttle with a known short backoff — the caller's
			// inline retry loop handles this without locking out
			// everything else.
			return resp, nil
		}
		// Nothing retries this request (the SDK path), so the next call
		// would go straight back to Spotify: hold every call for the
		// backoff Spotify asked for. It is short and known, so the
		// streak does not escalate.
		t.setUntil(time.Now().Add(time.Duration(wait) * time.Second))
		return resp, nil
	}
	t.arm(sentUntil, time.Duration(wait)*time.Second)
	return resp, nil
}

// arm sets the cooldown for a throttle whose Retry-After, if any, is wait.
// sentUntil is the deadline the caller saw before its request went out:
// when it has moved since, a parallel request of the same burst already
// counted this throttle, so only this response's own Retry-After is
// honoured; otherwise the streak escalates. Returns the deadline in force.
func (t *rateLimitTransport) arm(sentUntil int64, wait time.Duration) time.Time {
	cooldown := max(wait, rateLimitMinBackoff)
	t.armMu.Lock()
	defer t.armMu.Unlock()
	if t.until.Load() != sentUntil {
		// Same burst: honour this response's own Retry-After but don't
		// count it as another consecutive throttle.
		t.setUntil(time.Now().Add(min(cooldown, rateLimitMaxBackoff)))
		return time.Unix(0, t.until.Load())
	}
	// Exponential backoff: each consecutive 429 doubles the base cooldown.
	// Shift capped so overflow can't produce a negative Duration; the
	// result is clamped to rateLimitMaxBackoff regardless.
	n := t.consecutive.Add(1)
	shift := min(n-1, rateLimitMaxShift)
	cooldown *= 1 << shift
	if cooldown > rateLimitMaxBackoff || cooldown <= 0 {
		cooldown = rateLimitMaxBackoff
	}
	t.setUntil(time.Now().Add(cooldown))
	return time.Unix(0, t.until.Load())
}

// throttled records a throttle that an inline retry loop could not clear:
// three short Retry-After 429s in a row for one request are a sustained
// throttle, not a blip, so the cooldown is armed and escalated like any
// consecutive 429. startUntil is the deadline in force when the loop
// started: a cooldown armed since by another request, such as a parallel
// loop that ran out first, is the same throttle and is extended, not
// escalated again, as for a burst. Returns the deadline in force.
func (t *rateLimitTransport) throttled(startUntil int64) time.Time {
	return t.arm(startUntil, 0)
}

// mark returns the cooldown deadline in force now, for a later throttled
// call to tell whether a cooldown was armed in between.
func (t *rateLimitTransport) mark() int64 {
	return t.until.Load()
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

// inlineRetryKey is the context key withInlineRetry sets.
type inlineRetryKey struct{}

// withInlineRetry marks requests made with ctx as retried by their caller
// when Spotify answers 429 with a short Retry-After, as doWithRetry does,
// so the transport leaves the shared cooldown unset for them.
func withInlineRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, inlineRetryKey{}, true)
}

// retriesInline reports whether ctx was marked by withInlineRetry.
func retriesInline(ctx context.Context) bool {
	marked, _ := ctx.Value(inlineRetryKey{}).(bool)
	return marked
}
