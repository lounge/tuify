package spotify

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetPlaylists_OwnerFiltering(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  3,
		"items": []map[string]any{
			{"id": "p1", "name": "My Playlist", "owner": map[string]any{"id": "me", "display_name": "Me"}, "items": map[string]any{"total": 10}},
			{"id": "p2", "name": "Other Playlist", "owner": map[string]any{"id": "other", "display_name": "Other"}, "items": map[string]any{"total": 5}},
			{"id": "p3", "name": "Also Mine", "owner": map[string]any{"id": "me", "display_name": "Me"}, "items": map[string]any{"total": 20}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  10,
		"items": []map[string]any{
			{"id": "p1", "name": "Mine", "owner": map[string]any{"id": "me", "display_name": "Me"}, "items": map[string]any{"total": 5}},
			{"id": "p2", "name": "Theirs", "owner": map[string]any{"id": "other", "display_name": "Other"}, "items": map[string]any{"total": 3}},
			{"id": "p3", "name": "Theirs 2", "owner": map[string]any{"id": "other2", "display_name": "Other2"}, "items": map[string]any{"total": 1}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  2,
		"items": []map[string]any{
			{"id": "p1", "name": "Playlist A", "owner": map[string]any{"id": "a", "display_name": "A"}, "items": map[string]any{"total": 5}},
			{"id": "p2", "name": "Playlist B", "owner": map[string]any{"id": "b", "display_name": "B"}, "items": map[string]any{"total": 3}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.MarshalWrite(w, response)
	})

	// No userID and /me fails: degrade to returning all playlists.
	playlists, _, _, err := c.GetPlaylists(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(playlists) != 2 {
		t.Fatalf("expected 2 playlists (no filtering), got %d", len(playlists))
	}
}

func TestGetPlaylists_FetchesUserIDOnDemand(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  2,
		"items": []map[string]any{
			{"id": "p1", "name": "Mine", "owner": map[string]any{"id": "me", "display_name": "Me"}, "items": map[string]any{"total": 1}},
			{"id": "p2", "name": "Followed", "owner": map[string]any{"id": "other", "display_name": "Other"}, "items": map[string]any{"total": 1}},
		},
	}
	var meCalls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" {
			meCalls.Add(1)
			json.MarshalWrite(w, map[string]string{"id": "me"})
			return
		}
		json.MarshalWrite(w, response)
	})

	// The startup FetchUserID was skipped or failed; the first
	// GetPlaylists fetches the ID and filters out followed playlists.
	for range 2 {
		playlists, _, _, err := c.GetPlaylists(context.Background(), 0, 50)
		if err != nil {
			t.Fatalf("GetPlaylists: %v", err)
		}
		if len(playlists) != 1 || playlists[0].ID != "p1" {
			t.Fatalf("got %+v, want only the user's own playlist", playlists)
		}
	}
	if n := meCalls.Load(); n != 1 {
		t.Errorf("/v1/me fetched %d times, want once (then cached)", n)
	}
}

func TestGetPlaylistTracks(t *testing.T) {
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  3,
		"items": []map[string]any{
			{"item": map[string]any{
				"id": "t1", "uri": "spotify:track:t1", "name": "Track One",
				"duration_ms": 200000, "artists": []map[string]any{{"name": "Artist A"}},
				"album": map[string]any{"name": "Album X"},
			}},
			{"item": map[string]any{
				"id": "t2", "uri": "spotify:track:t2", "name": "Track Two",
				"duration_ms": 180000, "artists": []map[string]any{{"name": "Artist B"}},
				"album": map[string]any{"name": "Album Y"},
			}},
			// Empty item (e.g. deleted track) — should be filtered out
			{"item": map[string]any{
				"id": "", "uri": "", "name": "",
			}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	tracks, rawCount, more, err := c.GetPlaylistTracks(context.Background(), "playlist1", 0, 50)
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks (filtered empty), got %d", len(tracks))
	}
	// The dropped entry still occupies a slot in the playlist, so the
	// caller must advance its offset by the raw page size.
	if rawCount != 3 {
		t.Errorf("rawCount = %d, want 3 (page size before filtering)", rawCount)
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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  2,
		"items": []map[string]any{
			{"show": map[string]any{
				"id": "s1", "uri": "spotify:show:s1", "name": "Show One", "total_episodes": 50,
			}},
			{"show": map[string]any{
				"id": "s2", "uri": "spotify:show:s2", "name": "Show Two", "total_episodes": 100,
			}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	shows, _, more, err := c.GetSavedShows(context.Background(), 0, 50)
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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  1,
		"items": []map[string]any{
			{"id": "ep1", "uri": "spotify:episode:ep1", "name": "Episode One", "release_date": "2024-06-01", "duration_ms": 3600000},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, response)
	})

	eps, _, more, err := c.GetShowEpisodes(context.Background(), "show1", 0, 50)
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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  2,
		"items": []map[string]any{
			{"id": "a1", "uri": "spotify:album:a1", "name": "Album One", "release_date": "2020-01-01", "total_tracks": 10, "artists": []map[string]any{{"name": "The Artist"}}},
			{"id": "a2", "uri": "spotify:album:a2", "name": "Album Two", "release_date": "2022-06-15", "total_tracks": 8, "artists": []map[string]any{{"name": "The Artist"}}},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/artists/") {
			t.Errorf("expected /artists/ in path, got %s", r.URL.Path)
		}
		json.MarshalWrite(w, response)
	})

	albums, _, more, err := c.GetArtistAlbums(context.Background(), "artist1", 0, 50)
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
	t.Parallel()

	response := map[string]any{
		"offset": 0,
		"total":  2,
		"items": []map[string]any{
			{"id": "t1", "uri": "spotify:track:t1", "name": "Track One", "duration_ms": 200000, "artists": []map[string]any{{"name": "Artist"}}},
			{"id": "t2", "uri": "spotify:track:t2", "name": "Track Two", "duration_ms": 180000},
		},
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/albums/") {
			t.Errorf("expected /albums/ in path, got %s", r.URL.Path)
		}
		json.MarshalWrite(w, response)
	})

	tracks, _, more, err := c.GetAlbumTracks(context.Background(), "album1", 0, 50)
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

// IDs are interpolated into the URL path, so one carrying a slash must be
// escaped rather than change which endpoint is called.
func TestGetAlbumTracks_EscapesID(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.EscapedPath(), "/v1/albums/a%2F..%2Fb/tracks"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		json.MarshalWrite(w, map[string]any{"offset": 0, "total": 0, "items": []any{}})
	})
	if _, _, _, err := c.GetAlbumTracks(context.Background(), "a/../b", 0, 50); err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
}

// A failing GET /me used to be repeated on every page of a playlist fetch,
// doubling the request rate and the log lines. One failure pauses the
// lookup for the rest of the fetch.
func TestGetPlaylists_FailedUserIDNotRetriedPerPage(t *testing.T) {
	t.Parallel()

	var meCalls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me" {
			meCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.MarshalWrite(w, map[string]any{"offset": 0, "total": 100, "items": []map[string]any{
			{"id": "p1", "name": "A", "owner": map[string]any{"id": "x"}},
		}})
	})
	for page := range 2 {
		if _, _, _, err := c.GetPlaylists(context.Background(), page*50, 50); err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	if n := meCalls.Load(); n != 1 {
		t.Errorf("/v1/me requested %d times across two pages, want 1", n)
	}
}

// Spotify can return a null in place of an item it withholds. It decodes
// to a zero struct and used to become a blank row that plays nothing; it
// is dropped now, while the raw count still covers it so paging advances
// past it.
func TestPagedEndpoints_DropNullEntries(t *testing.T) {
	t.Parallel()

	t.Run("playlists without user ID", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/me" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write([]byte(`{"offset":0,"total":2,"items":[null,{"id":"p1","name":"A","owner":{"id":"x"}}]}`))
		})
		got, raw, _, err := c.GetPlaylists(context.Background(), 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "p1" || raw != 2 {
			t.Errorf("got %+v raw=%d, want only p1 and raw 2", got, raw)
		}
	})
	t.Run("show episodes", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"offset":0,"total":2,"items":[null,{"id":"e1","uri":"spotify:episode:e1","name":"E"}]}`))
		})
		got, raw, _, err := c.GetShowEpisodes(context.Background(), "s", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "e1" || raw != 2 {
			t.Errorf("got %+v raw=%d, want only e1 and raw 2", got, raw)
		}
	})
	// A page of nothing but nulls must still advance the caller: with
	// the filtered count, the next request asked for the same offset
	// again while more stayed true.
	t.Run("show episodes all null", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"offset":0,"total":5,"items":[null,null]}`))
		})
		got, raw, more, err := c.GetShowEpisodes(context.Background(), "s", 0, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 || raw != 2 || !more {
			t.Errorf("got %+v raw=%d more=%v, want none, raw 2 and more", got, raw, more)
		}
	})
	t.Run("saved shows", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"offset":0,"total":2,"items":[{"show":null},{"show":{"id":"s1","uri":"spotify:show:s1","name":"S"}}]}`))
		})
		got, raw, _, err := c.GetSavedShows(context.Background(), 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "s1" || raw != 2 {
			t.Errorf("got %+v raw=%d, want only s1 and raw 2", got, raw)
		}
	})
	t.Run("searched shows", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"shows":{"offset":0,"total":2,"items":[null,{"id":"s1","uri":"spotify:show:s1","name":"S"}]}}`))
		})
		got, raw, _, err := c.SearchShows(context.Background(), "q", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "s1" || raw != 2 {
			t.Errorf("got %+v raw=%d, want only s1 and raw 2", got, raw)
		}
	})
	t.Run("searched episodes", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"episodes":{"offset":0,"total":2,"items":[null,{"id":"e1","uri":"spotify:episode:e1","name":"E"}]}}`))
		})
		got, raw, _, err := c.SearchEpisodes(context.Background(), "q", 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "e1" || raw != 2 {
			t.Errorf("got %+v raw=%d, want only e1 and raw 2", got, raw)
		}
	})
}

// An empty page whose total runs ahead of what Spotify serves must not
// report more, or the playlist loader re-requests the same offset until
// its deadline.
func TestGetPlaylists_EmptyPageWithStaleTotalEnds(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"offset":40,"total":60,"items":[]}`))
	})
	c.userID = "me"
	_, raw, more, err := c.GetPlaylists(context.Background(), 40, 50)
	if err != nil {
		t.Fatal(err)
	}
	if more || raw != 0 {
		t.Errorf("empty page: more=%v raw=%d, want false and 0", more, raw)
	}
}
