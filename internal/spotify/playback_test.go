package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

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
		"device":        map[string]interface{}{"name": "MacBook Pro"},
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
	if state.DeviceName != "MacBook Pro" {
		t.Errorf("DeviceName: got %q, want %q", state.DeviceName, "MacBook Pro")
	}
}

func TestGetPlayerState_NoDevice(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": false,
		"progress_ms":   0,
		"item": map[string]interface{}{
			"name":        "Test Song",
			"uri":         "spotify:track:abc",
			"duration_ms": 200000,
			"artists":     []map[string]interface{}{{"name": "Artist"}},
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
	if state.DeviceName != "" {
		t.Errorf("DeviceName: got %q, want empty", state.DeviceName)
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

func TestGetPlayerState_WithAlbumImage(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": false,
		"progress_ms":   45000,
		"item": map[string]interface{}{
			"name":        "Image Track",
			"uri":         "spotify:track:img",
			"duration_ms": 300000,
			"artists":     []map[string]interface{}{{"name": "Visual Artist"}},
			"album": map[string]interface{}{
				"images": []map[string]interface{}{
					{"url": "https://img.spotify.com/large.jpg"},
					{"url": "https://img.spotify.com/medium.jpg"},
					{"url": "https://img.spotify.com/small.jpg"},
				},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("GetPlayerState: %v", err)
	}
	// Should pick the middle image
	if state.ImageURL != "https://img.spotify.com/medium.jpg" {
		t.Errorf("ImageURL: got %q, want medium image", state.ImageURL)
	}
}

func TestGetPlayerState_EpisodeImages(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": false,
		"progress_ms":   10000,
		"item": map[string]interface{}{
			"name":        "Episode With Images",
			"uri":         "spotify:episode:img",
			"duration_ms": 600000,
			"show":        map[string]interface{}{"name": "Image Show"},
			"images": []map[string]interface{}{
				{"url": "https://img.spotify.com/ep-large.jpg"},
				{"url": "https://img.spotify.com/ep-small.jpg"},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("GetPlayerState: %v", err)
	}
	// Should pick middle of episode images (index 1 for 2 images)
	if state.ImageURL != "https://img.spotify.com/ep-small.jpg" {
		t.Errorf("ImageURL: got %q", state.ImageURL)
	}
}

func TestGetPlayerState_WithContext(t *testing.T) {
	response := map[string]interface{}{
		"is_playing":    true,
		"shuffle_state": false,
		"progress_ms":   0,
		"context": map[string]interface{}{
			"uri": "spotify:playlist:abc123",
		},
		"item": map[string]interface{}{
			"name":        "Context Track",
			"uri":         "spotify:track:ctx",
			"duration_ms": 200000,
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	state, err := c.GetPlayerState(context.Background())
	if err != nil {
		t.Fatalf("GetPlayerState: %v", err)
	}
	if state.ContextURI != "spotify:playlist:abc123" {
		t.Errorf("ContextURI: got %q", state.ContextURI)
	}
}

func TestPlayOpts_WithDeviceID(t *testing.T) {
	opts := playOpts("device123")
	if opts.DeviceID == nil {
		t.Fatal("DeviceID should not be nil")
	}
}

func TestPlayOpts_EmptyDeviceID(t *testing.T) {
	opts := playOpts("")
	if opts.DeviceID != nil {
		t.Error("DeviceID should be nil for empty string")
	}
}
