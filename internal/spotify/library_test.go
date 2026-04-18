package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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
