package spotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"
)

// recordedRequest is what a Spotify endpoint saw: method, path, query and
// the decoded JSON body. An empty body and `{}` both record as nil: to the
// Web API they mean the same thing, "no parameters".
type recordedRequest struct {
	Method string
	Path   string
	Query  map[string]string
	Body   map[string]any
}

// newRecordingClient returns a Client whose every request is recorded and
// answered with 204 No Content, plus a func that returns the recording.
func newRecordingClient(t *testing.T) (*Client, func() []recordedRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []recordedRequest
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{Method: r.Method, Path: r.URL.Path, Query: map[string]string{}}
		for k := range r.URL.Query() {
			rec.Query[k] = r.URL.Query().Get(k)
		}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &rec.Body); err != nil {
				t.Errorf("%s %s: body is not JSON: %q", r.Method, r.URL.Path, b)
			}
		}
		if len(rec.Body) == 0 {
			rec.Body = nil
		}
		mu.Lock()
		reqs = append(reqs, rec)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return c, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), reqs...)
	}
}

// TestPlaybackControls_RequestShape pins each playback control to the Web
// API request it must send: method, endpoint, device_id and body. Error
// handling is covered separately by the sdk_errors tests.
func TestPlaybackControls_RequestShape(t *testing.T) {
	t.Parallel()

	dev := map[string]string{"device_id": "dev"}
	tests := []struct {
		name string
		call func(context.Context, *Client) error
		want []recordedRequest
	}{
		{"Play single item", func(ctx context.Context, c *Client) error {
			return c.Play(ctx, "spotify:track:1", "", "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/play", dev, map[string]any{
			"uris": []any{"spotify:track:1"},
		}}}},
		{"Play in context", func(ctx context.Context, c *Client) error {
			return c.Play(ctx, "spotify:track:1", "spotify:playlist:9", "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/play", dev, map[string]any{
			"context_uri": "spotify:playlist:9",
			"offset":      map[string]any{"uri": "spotify:track:1"},
		}}}},
		{"PlayQueue", func(ctx context.Context, c *Client) error {
			return c.PlayQueue(ctx, []string{"spotify:track:1", "spotify:track:2"}, "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/play", dev, map[string]any{
			"uris":   []any{"spotify:track:1", "spotify:track:2"},
			"offset": map[string]any{"uri": "spotify:track:1"},
		}}}},
		{"Resume", func(ctx context.Context, c *Client) error {
			return c.Resume(ctx, "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/play", dev, nil}}},
		{"Pause", func(ctx context.Context, c *Client) error {
			return c.Pause(ctx, "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/pause", dev, nil}}},
		{"Stop pauses then seeks to 0", func(ctx context.Context, c *Client) error {
			return c.Stop(ctx, "dev")
		}, []recordedRequest{
			{"PUT", "/v1/me/player/pause", dev, nil},
			{"PUT", "/v1/me/player/seek", map[string]string{"device_id": "dev", "position_ms": "0"}, nil},
		}},
		{"Next", func(ctx context.Context, c *Client) error {
			return c.Next(ctx, "dev")
		}, []recordedRequest{{"POST", "/v1/me/player/next", dev, nil}}},
		{"Previous", func(ctx context.Context, c *Client) error {
			return c.Previous(ctx, "dev")
		}, []recordedRequest{{"POST", "/v1/me/player/previous", dev, nil}}},
		{"Shuffle on", func(ctx context.Context, c *Client) error {
			return c.Shuffle(ctx, true, "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/shuffle", map[string]string{"device_id": "dev", "state": "true"}, nil}}},
		{"Seek", func(ctx context.Context, c *Client) error {
			return c.Seek(ctx, 61000, "dev")
		}, []recordedRequest{{"PUT", "/v1/me/player/seek", map[string]string{"device_id": "dev", "position_ms": "61000"}, nil}}},
		{"no device targets the active one", func(ctx context.Context, c *Client) error {
			return c.Pause(ctx, "")
		}, []recordedRequest{{"PUT", "/v1/me/player/pause", map[string]string{}, nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, recorded := newRecordingClient(t)
			if err := tc.call(t.Context(), c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := recorded(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("requests:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}
