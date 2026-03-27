package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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

func TestGetPlaylistTracks(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  3,
		"items": []map[string]interface{}{
			{"item": map[string]interface{}{
				"id": "t1", "uri": "spotify:track:t1", "name": "Track One",
				"duration_ms": 200000, "artists": []map[string]interface{}{{"name": "Artist A"}},
				"album": map[string]interface{}{"name": "Album X"},
			}},
			{"item": map[string]interface{}{
				"id": "t2", "uri": "spotify:track:t2", "name": "Track Two",
				"duration_ms": 180000, "artists": []map[string]interface{}{{"name": "Artist B"}},
				"album": map[string]interface{}{"name": "Album Y"},
			}},
			// Empty item (e.g. deleted track) — should be filtered out
			{"item": map[string]interface{}{
				"id": "", "uri": "", "name": "",
			}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	tracks, more, err := c.GetPlaylistTracks(context.Background(), "playlist1", 0, 50)
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks (filtered empty), got %d", len(tracks))
	}
	if tracks[0].Name != "Track One" {
		t.Errorf("track 0 name: got %q", tracks[0].Name)
	}
	if tracks[0].Artist != "Artist A" {
		t.Errorf("track 0 artist: got %q", tracks[0].Artist)
	}
	if more {
		t.Error("expected more=false (offset 0 + 3 items = total 3)")
	}
}

func TestGetSavedShows(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  2,
		"items": []map[string]interface{}{
			{"show": map[string]interface{}{
				"id": "s1", "uri": "spotify:show:s1", "name": "Show One", "total_episodes": 50,
			}},
			{"show": map[string]interface{}{
				"id": "s2", "uri": "spotify:show:s2", "name": "Show Two", "total_episodes": 100,
			}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	shows, more, err := c.GetSavedShows(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("GetSavedShows: %v", err)
	}
	if len(shows) != 2 {
		t.Fatalf("expected 2 shows, got %d", len(shows))
	}
	if shows[0].Name != "Show One" || shows[0].TotalEpisodes != 50 {
		t.Errorf("show 0: got %+v", shows[0])
	}
	if more {
		t.Error("expected more=false")
	}
}

func TestGetShowEpisodes(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  1,
		"items": []map[string]interface{}{
			{"id": "ep1", "uri": "spotify:episode:ep1", "name": "Episode One", "release_date": "2024-06-01", "duration_ms": 3600000},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	eps, more, err := c.GetShowEpisodes(context.Background(), "show1", 0, 50)
	if err != nil {
		t.Fatalf("GetShowEpisodes: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}
	if eps[0].Name != "Episode One" {
		t.Errorf("episode name: got %q", eps[0].Name)
	}
	if eps[0].ReleaseDate != "2024-06-01" {
		t.Errorf("release date: got %q", eps[0].ReleaseDate)
	}
	if more {
		t.Error("expected more=false")
	}
}

func TestSearchTracks(t *testing.T) {
	response := map[string]interface{}{
		"tracks": map[string]interface{}{
			"offset": 0,
			"total":  1,
			"items": []map[string]interface{}{
				{"id": "t1", "uri": "spotify:track:t1", "name": "Found Track",
					"duration_ms": 210000, "artists": []map[string]interface{}{{"name": "Searcher"}},
					"album": map[string]interface{}{"name": "Search Album"}},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "type=track") {
			t.Errorf("expected type=track in query, got %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	tracks, more, err := c.SearchTracks(context.Background(), "test query", 0, 20)
	if err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(tracks))
	}
	if tracks[0].Name != "Found Track" {
		t.Errorf("track name: got %q", tracks[0].Name)
	}
	if more {
		t.Error("expected more=false")
	}
}

func TestSearchEpisodes(t *testing.T) {
	response := map[string]interface{}{
		"episodes": map[string]interface{}{
			"offset": 0,
			"total":  1,
			"items": []map[string]interface{}{
				{"id": "ep1", "uri": "spotify:episode:ep1", "name": "Found Episode", "release_date": "2024-01-01", "duration_ms": 1800000},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	eps, _, err := c.SearchEpisodes(context.Background(), "podcast", 0, 20)
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(eps) != 1 || eps[0].Name != "Found Episode" {
		t.Errorf("unexpected episodes: %+v", eps)
	}
}

func TestSearchAlbums(t *testing.T) {
	response := map[string]interface{}{
		"albums": map[string]interface{}{
			"offset": 0,
			"total":  1,
			"items": []map[string]interface{}{
				{"id": "a1", "uri": "spotify:album:a1", "name": "Found Album",
					"release_date": "2023-05-15", "total_tracks": 12,
					"artists": []map[string]interface{}{{"name": "Album Artist"}}},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	albums, _, err := c.SearchAlbums(context.Background(), "album query", 0, 20)
	if err != nil {
		t.Fatalf("SearchAlbums: %v", err)
	}
	if len(albums) != 1 || albums[0].Name != "Found Album" {
		t.Errorf("unexpected albums: %+v", albums)
	}
	if albums[0].Artist != "Album Artist" {
		t.Errorf("artist: got %q", albums[0].Artist)
	}
}

func TestSearchArtists(t *testing.T) {
	response := map[string]interface{}{
		"artists": map[string]interface{}{
			"offset": 0,
			"total":  1,
			"items": []map[string]interface{}{
				{"id": "ar1", "uri": "spotify:artist:ar1", "name": "Found Artist", "genres": []string{"rock", "indie"}},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	artists, _, err := c.SearchArtists(context.Background(), "artist query", 0, 20)
	if err != nil {
		t.Fatalf("SearchArtists: %v", err)
	}
	if len(artists) != 1 || artists[0].Name != "Found Artist" {
		t.Errorf("unexpected artists: %+v", artists)
	}
	if len(artists[0].Genres) != 2 {
		t.Errorf("genres: got %v", artists[0].Genres)
	}
}

func TestSearchShows(t *testing.T) {
	response := map[string]interface{}{
		"shows": map[string]interface{}{
			"offset": 0,
			"total":  1,
			"items": []map[string]interface{}{
				{"id": "s1", "uri": "spotify:show:s1", "name": "Found Show", "total_episodes": 42},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	shows, _, err := c.SearchShows(context.Background(), "show query", 0, 20)
	if err != nil {
		t.Fatalf("SearchShows: %v", err)
	}
	if len(shows) != 1 || shows[0].Name != "Found Show" {
		t.Errorf("unexpected shows: %+v", shows)
	}
}

func TestGetArtistAlbums(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  2,
		"items": []map[string]interface{}{
			{"id": "a1", "uri": "spotify:album:a1", "name": "Album One", "release_date": "2020-01-01", "total_tracks": 10, "artists": []map[string]interface{}{{"name": "The Artist"}}},
			{"id": "a2", "uri": "spotify:album:a2", "name": "Album Two", "release_date": "2022-06-15", "total_tracks": 8, "artists": []map[string]interface{}{{"name": "The Artist"}}},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/artists/") {
			t.Errorf("expected /artists/ in path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	albums, more, err := c.GetArtistAlbums(context.Background(), "artist1", 0, 50)
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}
	if albums[0].Name != "Album One" {
		t.Errorf("album 0 name: got %q", albums[0].Name)
	}
	if more {
		t.Error("expected more=false")
	}
}

func TestGetAlbumTracks(t *testing.T) {
	response := map[string]interface{}{
		"offset": 0,
		"total":  2,
		"items": []map[string]interface{}{
			{"id": "t1", "uri": "spotify:track:t1", "name": "Track One", "duration_ms": 200000, "artists": []map[string]interface{}{{"name": "Artist"}}},
			{"id": "t2", "uri": "spotify:track:t2", "name": "Track Two", "duration_ms": 180000},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/albums/") {
			t.Errorf("expected /albums/ in path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	tracks, more, err := c.GetAlbumTracks(context.Background(), "album1", 0, 50)
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].Name != "Track One" {
		t.Errorf("track 0 name: got %q", tracks[0].Name)
	}
	if more {
		t.Error("expected more=false")
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

func TestSearchTracks_Pagination(t *testing.T) {
	response := map[string]interface{}{
		"tracks": map[string]interface{}{
			"offset": 0,
			"total":  50,
			"items": []map[string]interface{}{
				{"id": "t1", "uri": "spotify:track:t1", "name": "Track 1", "duration_ms": 100000},
			},
		},
	}

	c, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response)
	})
	defer cleanup()

	_, more, err := c.SearchTracks(context.Background(), "query", 0, 1)
	if err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	if !more {
		t.Error("expected more=true when total > offset+items")
	}
}

func TestHasMore(t *testing.T) {
	tests := []struct {
		offset, count, total int
		want                 bool
	}{
		{0, 10, 10, false},
		{0, 10, 20, true},
		{10, 10, 20, false},
		{0, 0, 0, false},
		{5, 5, 11, true},
	}
	for _, tt := range tests {
		got := hasMore(tt.offset, tt.count, tt.total)
		if got != tt.want {
			t.Errorf("hasMore(%d, %d, %d) = %v, want %v", tt.offset, tt.count, tt.total, got, tt.want)
		}
	}
}

func TestFirstArtist(t *testing.T) {
	if got := firstArtist(nil); got != "" {
		t.Errorf("firstArtist(nil) = %q, want empty", got)
	}
	if got := firstArtist([]rawArtistRef{}); got != "" {
		t.Errorf("firstArtist([]) = %q, want empty", got)
	}
	if got := firstArtist([]rawArtistRef{{Name: "A"}, {Name: "B"}}); got != "A" {
		t.Errorf("firstArtist = %q, want %q", got, "A")
	}
}

func TestConvertArtists(t *testing.T) {
	raw := []rawArtist{
		{ID: "ar1", URI: "spotify:artist:ar1", Name: "Artist One", Genres: []string{"rock"}},
		{ID: "ar2", URI: "spotify:artist:ar2", Name: "Artist Two", Genres: nil},
	}
	artists := convertArtists(raw)
	if len(artists) != 2 {
		t.Fatalf("expected 2 artists, got %d", len(artists))
	}
	if artists[0].Name != "Artist One" || len(artists[0].Genres) != 1 {
		t.Errorf("artist 0: got %+v", artists[0])
	}
	if artists[1].Genres != nil {
		t.Errorf("artist 1 genres: got %v, want nil", artists[1].Genres)
	}
}

func TestConvertArtists_Empty(t *testing.T) {
	artists := convertArtists(nil)
	if artists != nil {
		t.Errorf("expected nil, got %v", artists)
	}
}

func TestConvertShows(t *testing.T) {
	raw := []rawShow{
		{ID: "s1", URI: "spotify:show:s1", Name: "Show One", TotalEpisodes: 50},
	}
	shows := convertShows(raw)
	if len(shows) != 1 {
		t.Fatalf("expected 1 show, got %d", len(shows))
	}
	if shows[0].TotalEpisodes != 50 {
		t.Errorf("TotalEpisodes: got %d", shows[0].TotalEpisodes)
	}
}

func TestConvertShows_Empty(t *testing.T) {
	shows := convertShows(nil)
	if shows != nil {
		t.Errorf("expected nil, got %v", shows)
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
