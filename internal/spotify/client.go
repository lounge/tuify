package spotify

import (
	"context"
	"encoding/json"
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
func New(spClient *sp.Client, httpClient *http.Client) *Client {
	return &Client{sp: spClient, httpClient: httpClient}
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

// doWithRetry performs a GET request with 429 retry logic. Returns the
// response body, status code, and an error for non-2xx responses. The
// error is an *APIError for HTTP-level failures; callers that need to
// treat a specific status as non-error (e.g. 204) should check for it
// via errors.As before propagating.
func (c *Client) doWithRetry(ctx context.Context, url string) ([]byte, int, error) {
	for attempts := 0; attempts < 3; attempts++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			if wait > 10 {
				return nil, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), URL: url}
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
