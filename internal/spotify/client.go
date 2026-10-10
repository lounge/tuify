package spotify

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	sp "github.com/zmb3/spotify/v2"
)

// Operation names recorded in APIError.Endpoint for SDK-path failures, where
// the SDK does not expose the request URL.
const (
	opPlay     = "PUT /me/player/play"
	opPause    = "PUT /me/player/pause"
	opSeek     = "PUT /me/player/seek"
	opNext     = "POST /me/player/next"
	opPrevious = "POST /me/player/previous"
	opShuffle  = "PUT /me/player/shuffle"
	opDevices  = "GET /me/player/devices"
	opTransfer = "PUT /me/player"
)

// userIDRetryInterval is how long ownUserID waits after a failed GET /me
// before asking again. A playlist fetch loops over several pages in a
// row; without the pause every page would repeat the failing request and
// its log line.
const userIDRetryInterval = time.Minute

// PlayIntent is whether the user wants playback running, as far as the
// UI knows. The librespot reconnect handler reads it to decide whether
// the playback it moves back to tuify should play.
type PlayIntent int32

const (
	// PlayIntentUnknown: nothing has been played or paused yet this run.
	PlayIntentUnknown PlayIntent = iota
	// PlayIntentPlaying: the user started playback or it was seen running.
	PlayIntentPlaying
	// PlayIntentPaused: the user paused or stopped playback.
	PlayIntentPaused
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
	userMu          sync.Mutex // guards userID and userIDRetryAt
	userID          string
	userIDRetryAt   time.Time // earliest time ownUserID retries a failed GET /me
	preferredDevice string    // set by WithPreferredDevice; immutable after New

	// DeviceOverridden is set when the user manually switches playback to
	// another device in Spotify. Checked by the librespot OnReconnect
	// callback to avoid stealing playback back.
	DeviceOverridden atomic.Bool

	// playIntent is whether the user wants playback running: one of the
	// PlayIntent values, stored as int32. See SetPlayIntent.
	playIntent atomic.Int32
}

// New constructs a Client from the auth-wrapped httpClient.
//
// New first installs a rate-limit gate on httpClient.Transport, over the
// error-shape layer that gives every error response Spotify's JSON form,
// then builds the zmb3 SDK client on top of that same *http.Client. SDK
// calls (playback control, devices) and raw REST calls therefore share
// token refresh, the 429 cooldown and the error shape by construction,
// rather than relying on the caller to wire both clients to one transport.
func New(httpClient *http.Client, opts ...Option) *Client {
	rl := newRateLimitTransport(newErrorShapeTransport(httpClient.Transport))
	httpClient.Transport = rl
	c := &Client{sp: sp.New(httpClient), httpClient: httpClient, rl: rl}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetPlayIntent records whether the user wants playback running. The UI
// sets it from the user's own play, pause and stop actions and from polls
// that report playback running; not from a poll that reports it paused,
// since a dropped librespot session looks exactly like that.
func (c *Client) SetPlayIntent(playing bool) {
	v := PlayIntentPaused
	if playing {
		v = PlayIntentPlaying
	}
	c.playIntent.Store(int32(v))
}

// PlayIntent reports the last intent SetPlayIntent recorded.
func (c *Client) PlayIntent() PlayIntent {
	return PlayIntent(c.playIntent.Load())
}

// Option configures a Client in New.
type Option func(*Client)

// WithPreferredDevice names the Spotify Connect device (tuify's own
// librespot) that FindDevice prefers and that the UI treats as home.
func WithPreferredDevice(name string) Option {
	return func(c *Client) { c.preferredDevice = name }
}

// PreferredDevice returns the device name set by WithPreferredDevice, or
// "" when there is none. It never changes after New, so it is safe to read
// from any goroutine.
func (c *Client) PreferredDevice() string { return c.preferredDevice }

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
// round trip. Optional: if it was skipped or failed, the first method that
// needs the ID fetches it itself.
func (c *Client) FetchUserID(ctx context.Context) error {
	var me struct {
		ID string `json:"id"`
	}
	if err := c.apiGet(ctx, "https://api.spotify.com/v1/me", &me); err != nil {
		return err
	}
	c.userMu.Lock()
	c.userID = me.ID
	c.userMu.Unlock()
	return nil
}

// ownUserID returns the cached user ID, fetching it on first use if the
// startup fetch failed. Returns "" if it still can't be fetched, in which
// case callers skip ownership filtering rather than fail. A failed fetch
// is not repeated for userIDRetryInterval, so a paged fetch does not ask
// again on every page; a cancelled context is the caller leaving, not a
// failure, and does not start the pause.
func (c *Client) ownUserID(ctx context.Context) string {
	c.userMu.Lock()
	id, retryAt := c.userID, c.userIDRetryAt
	c.userMu.Unlock()
	if id != "" {
		return id
	}
	if time.Now().Before(retryAt) {
		return ""
	}
	if err := c.FetchUserID(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return ""
		}
		log.Printf("[spotify] could not fetch user ID, not filtering by owner for %v: %v", userIDRetryInterval, err)
		c.userMu.Lock()
		c.userIDRetryAt = time.Now().Add(userIDRetryInterval)
		c.userMu.Unlock()
		return ""
	}
	c.userMu.Lock()
	defer c.userMu.Unlock()
	return c.userID
}

// APIError is returned by every Client method for non-2xx responses from
// Spotify, on both the raw REST path and the SDK path. It carries the status
// code and (truncated) response body so callers can distinguish error shapes
// (e.g. 404 for "no active device") without re-parsing.
//
// Endpoint identifies the failed call: the full request URL on the raw REST
// path, or an operation name such as "PUT /me/player/play" on the SDK path,
// where the SDK does not expose the URL. For SDK calls Body holds Spotify's
// error message.
//
// Err is the underlying cause when it is one of this package's own types
// (currently *RateLimitedError for a cooldown short-circuit) and is reachable
// via errors.As. SDK error types are deliberately not wrapped, so callers
// never depend on the zmb3 SDK through this error.
type APIError struct {
	Status   int
	Body     []byte
	Endpoint string
	Err      error
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Spotify API %d: %s", e.Status, e.Body)
}

// Unwrap returns the underlying cause, if any.
func (e *APIError) Unwrap() error {
	return e.Err
}

// doWithRetry performs a GET request with inline 429 retry for short
// Retry-After throttles. Long throttles and missing-Retry-After 429s arm
// the shared rateLimitTransport cooldown instead, so subsequent calls
// short-circuit before hitting the network; so does a request whose
// inline retries are all throttled, since three short 429s in a row are
// a sustained throttle that every caller would otherwise spend its own
// deadline sleeping through. Returns an *APIError for non-2xx responses;
// callers can errors.As to inspect the status.
func (c *Client) doWithRetry(ctx context.Context, url string) ([]byte, int, error) {
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Translate transport short-circuits into an APIError so callers
			// see the same shape as a real 429 from Spotify.
			if rle, ok := errors.AsType[*RateLimitedError](err); ok {
				return nil, http.StatusTooManyRequests, &APIError{
					Status:   http.StatusTooManyRequests,
					Body:     []byte(rle.Error()),
					Endpoint: url,
					Err:      rle,
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
				return nil, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), Endpoint: url}
			}
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			// The transport arms the cooldown for a missing, zero or
			// negative Retry-After, so this path only sees positive
			// values; the floor keeps a retry from ever being immediate
			// should that change.
			wait = max(wait, 1)
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
		return body, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), Endpoint: url}
	}
	apiErr := &APIError{
		Status:   http.StatusTooManyRequests,
		Body:     []byte("rate limited after retries"),
		Endpoint: url,
	}
	if c.rl != nil {
		apiErr.Err = &RateLimitedError{Until: c.rl.throttled()}
	}
	return nil, http.StatusTooManyRequests, apiErr
}

// wrapSDKErr normalizes an error from the zmb3 SDK into an *APIError so SDK
// methods honor the same error contract as the raw REST path. op names the
// operation (e.g. "PUT /me/player/play") and is stored in APIError.Endpoint.
//
// The SDK reports non-2xx responses as the value type sp.Error (every
// error response has Spotify's JSON shape by the time the SDK reads it;
// see errorShapeTransport), and a cooldown short-circuit from
// rateLimitTransport as *RateLimitedError wrapped in *url.Error. Anything
// else (context cancellation, network failures) is returned unchanged.
func wrapSDKErr(err error, op string) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*APIError](err); ok {
		return err
	}
	if rle, ok := errors.AsType[*RateLimitedError](err); ok {
		return &APIError{
			Status:   http.StatusTooManyRequests,
			Body:     []byte(rle.Error()),
			Endpoint: op,
			Err:      rle,
		}
	}
	if se, ok := errors.AsType[sp.Error](err); ok {
		return &APIError{
			Status:   se.Status,
			Body:     truncateForLog([]byte(se.Message)),
			Endpoint: op,
		}
	}
	return err
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
	const maxLen = 500
	if len(b) <= maxLen {
		return b
	}
	cut := maxLen
	for cut > 0 {
		r, _ := utf8.DecodeLastRune(b[:cut])
		if r != utf8.RuneError {
			break
		}
		cut--
	}
	return append(append([]byte(nil), b[:cut]...), "…"...)
}
