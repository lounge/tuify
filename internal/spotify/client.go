package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	sp "github.com/zmb3/spotify/v2"
)

// Client wraps the zmb3 Spotify SDK with the higher-level operations tuify
// needs (playlists, search, player control, device selection). Safe for
// concurrent use by multiple goroutines.
//
// Split across files by responsibility: this file holds the Client type
// plus the shared HTTP/JSON plumbing; playback.go, devices.go, library.go,
// and search.go hold the operation methods; types.go holds the domain
// value types and JSON raw shapes.
type Client struct {
	sp              *sp.Client
	httpClient      *http.Client
	rl              *rateLimitTransport
	userID          string
	PreferredDevice string // if set, FindDevice prefers this device name

	// DeviceOverridden is set when the user manually switches playback to
	// another device in Spotify. Checked by the librespot OnReconnect
	// callback to avoid stealing playback back.
	DeviceOverridden atomic.Bool
}

// New constructs a Client. spClient handles SDK-level calls (playback
// control, devices); httpClient is used for raw REST calls that the SDK
// doesn't expose and must be the same auth-wrapped client so both paths
// share token refresh.
//
// New installs a shared rate-limit gate on httpClient.Transport so SDK
// and raw paths honor the same cooldown when Spotify returns 429.
func New(spClient *sp.Client, httpClient *http.Client) *Client {
	rl := newRateLimitTransport(httpClient.Transport)
	httpClient.Transport = rl
	return &Client{sp: spClient, httpClient: httpClient, rl: rl}
}

// RateLimitWait reports the remaining cooldown imposed by Spotify, or zero
// when not rate limited. Callers (e.g. the now-playing poll loop) use this
// to skip API calls and reschedule themselves past the cooldown instead of
// hammering the gate.
func (c *Client) RateLimitWait() time.Duration {
	if c.rl == nil {
		return 0
	}
	return c.rl.wait()
}

// IsRateLimited reports whether the client is currently in a rate-limit
// cooldown. Equivalent to RateLimitWait() > 0; provided for readability at
// call sites that don't need the duration.
func (c *Client) IsRateLimited() bool {
	return c.RateLimitWait() > 0
}

// FetchUserID caches the authenticated user's ID on the client so later
// calls (e.g. GetPlaylists) can filter by ownership without an extra
// round trip. Safe to skip; dependent methods degrade gracefully.
func (c *Client) FetchUserID(ctx context.Context) error {
	var me struct {
		ID string `json:"id"`
	}
	if err := c.apiGet(ctx, "https://api.spotify.com/v1/me", &me); err != nil {
		return err
	}
	c.userID = me.ID
	return nil
}

// APIError is returned by doWithRetry for non-2xx responses. It carries the
// status code and (truncated) response body so callers can distinguish error
// shapes (e.g. StatusNoContent for "no active playback") without re-parsing.
type APIError struct {
	Status int
	Body   []byte
	URL    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Spotify API %d: %s", e.Status, e.Body)
}

// doWithRetry performs a GET request with inline 429 retry for short
// Retry-After throttles. Long throttles and missing-Retry-After 429s arm
// the shared rateLimitTransport cooldown instead, so subsequent calls
// short-circuit before hitting the network. Returns an *APIError for
// non-2xx responses; callers can errors.As to inspect the status.
func (c *Client) doWithRetry(ctx context.Context, url string) ([]byte, int, error) {
	for attempts := 0; attempts < 3; attempts++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Translate transport short-circuits into an APIError so callers
			// see the same shape as a real 429 from Spotify.
			var rle *RateLimitedError
			if errors.As(err, &rle) {
				return nil, http.StatusTooManyRequests, &APIError{
					Status: http.StatusTooManyRequests,
					Body:   []byte(rle.Error()),
					URL:    url,
				}
			}
			return nil, 0, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			// If the transport just armed a cooldown for this 429 (long or
			// missing Retry-After), don't retry inline — the next attempt
			// would short-circuit anyway.
			if c.IsRateLimited() {
				return nil, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), URL: url}
			}
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			select {
			case <-time.After(time.Duration(wait) * time.Second):
				continue
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, resp.StatusCode, nil
		}
		log.Printf("[spotify] %s %d body=%s", url, resp.StatusCode, truncateForLog(body))
		return body, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), URL: url}
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("Spotify API 429: rate limited after retries")
}

func (c *Client) apiGet(ctx context.Context, url string, result any) error {
	body, _, err := c.doWithRetry(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, result)
}

// truncateForLog caps a response body for logging/error storage. Large
// bodies can contain sensitive tokens or flood logs. The cut is aligned to
// a UTF-8 rune boundary so we never emit a malformed byte sequence followed
// by the ellipsis.
func truncateForLog(b []byte) []byte {
	const max = 500
	if len(b) <= max {
		return b
	}
	cut := max
	for cut > 0 {
		r, _ := utf8.DecodeLastRune(b[:cut])
		if r != utf8.RuneError {
			break
		}
		cut--
	}
	return append(append([]byte(nil), b[:cut]...), "…"...)
}
