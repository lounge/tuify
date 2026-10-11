package spotify

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lounge/tuify/internal/termsafe"
)

func TestSearchTracks(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"tracks": map[string]any{
			"offset": 0,
			"total":  1,
			"items": []map[string]any{
				{"id": "t1", "uri": "spotify:track:t1", "name": "Found Track",
					"duration_ms": 210000, "artists": []map[string]any{{"name": "Searcher"}},
					"album": map[string]any{"name": "Search Album"}},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "type=track") {
			t.Errorf("expected type=track in query, got %s", r.URL.RawQuery)
		}
		json.MarshalWrite(w, response)
	})

	tracks, _, more, err := c.SearchTracks(context.Background(), "test query", 0, 20)
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
	t.Parallel()

	response := map[string]any{
		"episodes": map[string]any{
			"offset": 0,
			"total":  1,
			"items": []map[string]any{
				{"id": "ep1", "uri": "spotify:episode:ep1", "name": "Found Episode", "release_date": "2024-01-01", "duration_ms": 1800000},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	eps, _, _, err := c.SearchEpisodes(context.Background(), "podcast", 0, 20)
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(eps) != 1 || eps[0].Name != "Found Episode" {
		t.Errorf("unexpected episodes: %+v", eps)
	}
}

func TestSearchAlbums(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"albums": map[string]any{
			"offset": 0,
			"total":  1,
			"items": []map[string]any{
				{"id": "a1", "uri": "spotify:album:a1", "name": "Found Album",
					"release_date": "2023-05-15", "total_tracks": 12,
					"artists": []map[string]any{{"name": "Album Artist"}}},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	albums, _, _, err := c.SearchAlbums(context.Background(), "album query", 0, 20)
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
	t.Parallel()

	response := map[string]any{
		"artists": map[string]any{
			"offset": 0,
			"total":  1,
			"items": []map[string]any{
				{"id": "ar1", "uri": "spotify:artist:ar1", "name": "Found Artist", "genres": []string{"rock", "indie"}},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	artists, _, _, err := c.SearchArtists(context.Background(), "artist query", 0, 20)
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
	t.Parallel()

	response := map[string]any{
		"shows": map[string]any{
			"offset": 0,
			"total":  1,
			"items": []map[string]any{
				{"id": "s1", "uri": "spotify:show:s1", "name": "Found Show", "total_episodes": 42},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	shows, _, _, err := c.SearchShows(context.Background(), "show query", 0, 20)
	if err != nil {
		t.Fatalf("SearchShows: %v", err)
	}
	if len(shows) != 1 || shows[0].Name != "Found Show" {
		t.Errorf("unexpected shows: %+v", shows)
	}
}

func TestSearchTracks_Pagination(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"tracks": map[string]any{
			"offset": 0,
			"total":  50,
			"items": []map[string]any{
				{"id": "t1", "uri": "spotify:track:t1", "name": "Track 1", "duration_ms": 100000},
			},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	_, _, more, err := c.SearchTracks(context.Background(), "query", 0, 1)
	if err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	if !more {
		t.Error("expected more=true when total > offset+items")
	}
}

// Search results are rendered straight into the terminal, so every string
// field each converter maps (names, artists, albums, release dates,
// genres) must have been through termsafe.Clean. One probe string carrying
// an OSC 52 clipboard write goes through every field of every search type.
func TestSearch_CleansText(t *testing.T) {
	t.Parallel()

	const evil = "\x1b]52;c;evil\x07name"
	clean := termsafe.Clean(evil)
	if clean == evil || strings.ContainsAny(clean, "\x1b\x07") {
		t.Fatalf("termsafe.Clean(%q) = %q; the probe string is not doing its job", evil, clean)
	}

	tests := []struct {
		name   string
		key    string
		item   map[string]any
		search func(*Client) (any, error)
		want   any
	}{
		{
			name: "tracks",
			key:  "tracks",
			item: map[string]any{"id": "t1", "uri": "spotify:track:t1", "name": evil, "duration_ms": 1000,
				"artists": []map[string]any{{"name": evil}}, "album": map[string]any{"name": evil}},
			search: func(c *Client) (any, error) { v, _, _, err := c.SearchTracks(t.Context(), "q", 0, 1); return v, err },
			want:   []Track{{ID: "t1", URI: "spotify:track:t1", Name: clean, Artist: clean, Album: clean, Duration: time.Second}},
		},
		{
			name: "albums",
			key:  "albums",
			item: map[string]any{"id": "a1", "uri": "spotify:album:a1", "name": evil, "release_date": evil,
				"total_tracks": 2, "artists": []map[string]any{{"name": evil}}},
			search: func(c *Client) (any, error) { v, _, _, err := c.SearchAlbums(t.Context(), "q", 0, 1); return v, err },
			want:   []Album{{ID: "a1", URI: "spotify:album:a1", Name: clean, Artist: clean, ReleaseDate: clean, TrackCount: 2}},
		},
		{
			name:   "artists",
			key:    "artists",
			item:   map[string]any{"id": "ar1", "uri": "spotify:artist:ar1", "name": evil, "genres": []string{evil, "rock"}},
			search: func(c *Client) (any, error) { v, _, _, err := c.SearchArtists(t.Context(), "q", 0, 1); return v, err },
			want:   []Artist{{ID: "ar1", URI: "spotify:artist:ar1", Name: clean, Genres: []string{clean, "rock"}}},
		},
		{
			name:   "episodes",
			key:    "episodes",
			item:   map[string]any{"id": "e1", "uri": "spotify:episode:e1", "name": evil, "release_date": evil, "duration_ms": 1000},
			search: func(c *Client) (any, error) { v, _, _, err := c.SearchEpisodes(t.Context(), "q", 0, 1); return v, err },
			want:   []Episode{{ID: "e1", URI: "spotify:episode:e1", Name: clean, ReleaseDate: clean, Duration: time.Second}},
		},
		{
			name:   "shows",
			key:    "shows",
			item:   map[string]any{"id": "s1", "uri": "spotify:show:s1", "name": evil, "total_episodes": 3},
			search: func(c *Client) (any, error) { v, _, _, err := c.SearchShows(t.Context(), "q", 0, 1); return v, err },
			want:   []Show{{ID: "s1", URI: "spotify:show:s1", Name: clean, TotalEpisodes: 3}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			response := map[string]any{tc.key: map[string]any{"offset": 0, "total": 1, "items": []map[string]any{tc.item}}}
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				json.MarshalWrite(w, response)
			})
			got, err := tc.search(c)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("search results:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// Spotify's search endpoint serves at most 1000 results (offset+limit must
// stay at or below 1000), so paging must stop at the cap even when total
// claims more; the next request would be a 400.
func TestSearch_StopsAtOffsetCap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		offset, n int
		want      bool
	}{
		{980, 10, true},  // ends at 990, still under the cap
		{990, 10, false}, // ends exactly at the cap
		{995, 5, false},
	} {
		items := make([]map[string]any, tc.n)
		for i := range items {
			items[i] = map[string]any{"id": "t", "uri": "spotify:track:t", "name": "T"}
		}
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			json.MarshalWrite(w, map[string]any{"tracks": map[string]any{"offset": tc.offset, "total": 50000, "items": items}})
		})
		_, _, more, err := c.SearchTracks(context.Background(), "q", tc.offset, tc.n)
		if err != nil {
			t.Fatalf("offset %d: %v", tc.offset, err)
		}
		if more != tc.want {
			t.Errorf("offset %d + %d items: more = %v, want %v", tc.offset, tc.n, more, tc.want)
		}
	}
}
