package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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
