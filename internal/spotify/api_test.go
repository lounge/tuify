package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lounge/tuify/internal/testutil"
)

// newTestClient creates a Client backed by a test HTTP server.
// The handler receives all requests. The returned cleanup function must be deferred.
func newTestClient(handler http.HandlerFunc) (*Client, func()) {
	srv := httptest.NewServer(handler)
	transport := &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}
	c := &Client{httpClient: &http.Client{Transport: transport}}
	return c, srv.Close
}

func TestGetPlaylists_OwnerFiltering(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  3,
		"items": []map[string]interface{}{
			{"id": "p1", "name": "My Playlist", "owner": map[string]interface{}{"id": "me", "display_name": "Me"}, "items": map[string]interface{}{"total": 10}},
			{"id": "p2", "name": "Other Playlist", "owner": map[string]interface{}{"id": "other", "display_name": "Other"}, "items": map[string]interface{}{"total": 5}},
			{"id": "p3", "name": "Also Mine", "owner": map[string]interface{}{"id": "me", "display_name": "Me"}, "items": map[string]interface{}{"total": 20}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	c.userID = "me"

	playlists, pageSize, hasMore, err := c.GetPlaylists(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should filter out "other" user's playlist
	if len(playlists) != 2 {
		t.Fatalf("expected 2 playlists, got %d", len(playlists))
	}
	if playlists[0].Name != "My Playlist" || playlists[1].Name != "Also Mine" {
		t.Errorf("wrong playlists: %+v", playlists)
	}
	if playlists[0].TrackCount != 10 {
		t.Errorf("track count: got %d, want 10", playlists[0].TrackCount)
	}

	// pageSize should be raw count (3), not filtered count (2)
	if pageSize != 3 {
		t.Errorf("pageSize: got %d, want 3", pageSize)
	}
	if hasMore {
		t.Error("hasMore should be false (offset 0 + 3 items = total 3)")
	}
}

func TestGetPlaylists_HasMoreWithFiltering(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  10,
		"items": []map[string]interface{}{
			{"id": "p1", "name": "Mine", "owner": map[string]interface{}{"id": "me", "display_name": "Me"}, "items": map[string]interface{}{"total": 5}},
			{"id": "p2", "name": "Theirs", "owner": map[string]interface{}{"id": "other", "display_name": "Other"}, "items": map[string]interface{}{"total": 3}},
			{"id": "p3", "name": "Theirs 2", "owner": map[string]interface{}{"id": "other2", "display_name": "Other2"}, "items": map[string]interface{}{"total": 1}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	c.userID = "me"

	playlists, pageSize, hasMore, err := c.GetPlaylists(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 playlist passes filter, but hasMore should still be true (3 < 10)
	if len(playlists) != 1 {
		t.Fatalf("expected 1 filtered playlist, got %d", len(playlists))
	}
	if pageSize != 3 {
		t.Errorf("pageSize should be raw count 3, got %d", pageSize)
	}
	if !hasMore {
		t.Error("hasMore should be true (offset 0 + 3 items < total 10)")
	}
}

func TestGetPlaylists_NoUserID(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  2,
		"items": []map[string]interface{}{
			{"id": "p1", "name": "Playlist A", "owner": map[string]interface{}{"id": "a", "display_name": "A"}, "items": map[string]interface{}{"total": 5}},
			{"id": "p2", "name": "Playlist B", "owner": map[string]interface{}{"id": "b", "display_name": "B"}, "items": map[string]interface{}{"total": 3}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	// No userID set — should return all playlists
	playlists, _, _, err := c.GetPlaylists(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(playlists) != 2 {
		t.Fatalf("expected 2 playlists (no filtering), got %d", len(playlists))
	}
}

func TestGetPlayerState_NoContent(t *testing.T) {
	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state for 204, got %+v", state)
	}
}

func TestGetPlayerState_NilItem(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    false,
		"shuffle_state": false,
		"progress_ms":   0,
		"item":          nil,
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state for null item, got %+v", state)
	}
}

func TestGetPlayerState_Playing(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": true,
		"progress_ms":   60000,
		"item": map[string]interface{}{
			"name":        "Test Song",
			"uri":         "spotify:track:abc",
			"duration_ms": 200000,
			"artists":     []map[string]interface{}{{"name": "Test Artist"}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if !state.Playing {
		t.Error("expected Playing=true")
	}
	if !state.Shuffling {
		t.Error("expected Shuffling=true")
	}
	if state.TrackName != "Test Song" {
		t.Errorf("TrackName: got %q", state.TrackName)
	}
	if state.ArtistName != "Test Artist" {
		t.Errorf("ArtistName: got %q", state.ArtistName)
	}
	if state.ProgressMs != 60000 {
		t.Errorf("ProgressMs: got %d", state.ProgressMs)
	}
	if state.DurationMs != 200000 {
		t.Errorf("DurationMs: got %d", state.DurationMs)
	}
}

func TestGetPlayerState_EpisodeWithShow(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": false,
		"progress_ms":   30000,
		"item": map[string]interface{}{
			"name":        "Episode Title",
			"uri":         "spotify:episode:xyz",
			"duration_ms": 1800000,
			"show":        map[string]interface{}{"name": "Podcast Name"},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.ArtistName != "Podcast Name" {
		t.Errorf("ArtistName for episode: got %q, want %q", state.ArtistName, "Podcast Name")
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
