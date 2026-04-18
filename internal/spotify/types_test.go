package spotify

import (
	"testing"
	"time"
)

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

func TestConvertTracks(t *testing.T) {
	raw := []rawTrack{
		{
			ID:       "t1",
			URI:      "spotify:track:t1",
			Name:     "Song One",
			Duration: 210000,
			Artists:  []rawArtistRef{{Name: "Artist A"}},
			Album: struct {
				Name string `json:"name"`
			}{Name: "Album X"},
		},
		{
			ID:       "t2",
			URI:      "spotify:track:t2",
			Name:     "Song Two",
			Duration: 180000,
		},
	}

	tracks := convertTracks(raw)

	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}

	// Track with artist and album
	if tracks[0].ID != "t1" || tracks[0].URI != "spotify:track:t1" || tracks[0].Name != "Song One" {
		t.Errorf("track 0 basic fields: got %+v", tracks[0])
	}
	if tracks[0].Artist != "Artist A" {
		t.Errorf("track 0 artist: got %q, want %q", tracks[0].Artist, "Artist A")
	}
	if tracks[0].Album != "Album X" {
		t.Errorf("track 0 album: got %q, want %q", tracks[0].Album, "Album X")
	}
	if tracks[0].Duration != 210*time.Second {
		t.Errorf("track 0 duration: got %v, want %v", tracks[0].Duration, 210*time.Second)
	}

	// Track without artist/album (e.g. album track endpoint)
	if tracks[1].Artist != "" {
		t.Errorf("track 1 artist: got %q, want empty", tracks[1].Artist)
	}
	if tracks[1].Album != "" {
		t.Errorf("track 1 album: got %q, want empty", tracks[1].Album)
	}
}

func TestConvertTracks_Empty(t *testing.T) {
	tracks := convertTracks(nil)
	if tracks != nil {
		t.Errorf("expected nil, got %v", tracks)
	}
}

func TestConvertAlbums(t *testing.T) {
	raw := []rawAlbum{
		{
			ID:          "a1",
			URI:         "spotify:album:a1",
			Name:        "Album One",
			ReleaseDate: "2023-05-15",
			TotalTracks: 12,
			Artists:     []rawArtistRef{{Name: "Artist B"}},
		},
		{
			ID:          "a2",
			URI:         "spotify:album:a2",
			Name:        "Album Two",
			ReleaseDate: "2020-01-01",
			TotalTracks: 8,
		},
	}

	albums := convertAlbums(raw)

	if len(albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(albums))
	}

	if albums[0].Artist != "Artist B" {
		t.Errorf("album 0 artist: got %q, want %q", albums[0].Artist, "Artist B")
	}
	if albums[0].TrackCount != 12 {
		t.Errorf("album 0 track count: got %d, want 12", albums[0].TrackCount)
	}
	if albums[0].ReleaseDate != "2023-05-15" {
		t.Errorf("album 0 release date: got %q", albums[0].ReleaseDate)
	}

	// No artists
	if albums[1].Artist != "" {
		t.Errorf("album 1 artist: got %q, want empty", albums[1].Artist)
	}
}

func TestConvertAlbums_Empty(t *testing.T) {
	albums := convertAlbums(nil)
	if albums != nil {
		t.Errorf("expected nil, got %v", albums)
	}
}

func TestConvertEpisodes(t *testing.T) {
	raw := []rawEpisode{
		{
			ID:          "e1",
			URI:         "spotify:episode:e1",
			Name:        "Episode One",
			ReleaseDate: "2024-03-01",
			DurationMs:  3600000,
		},
	}

	episodes := convertEpisodes(raw)

	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}

	ep := episodes[0]
	if ep.ID != "e1" || ep.URI != "spotify:episode:e1" || ep.Name != "Episode One" {
		t.Errorf("episode basic fields: got %+v", ep)
	}
	if ep.ReleaseDate != "2024-03-01" {
		t.Errorf("episode release date: got %q", ep.ReleaseDate)
	}
	if ep.Duration != time.Hour {
		t.Errorf("episode duration: got %v, want %v", ep.Duration, time.Hour)
	}
}

func TestConvertEpisodes_Empty(t *testing.T) {
	episodes := convertEpisodes(nil)
	if episodes != nil {
		t.Errorf("expected nil, got %v", episodes)
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
