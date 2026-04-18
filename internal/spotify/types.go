package spotify

import "time"

// Domain types returned by the Client. These are the flattened,
// UI-friendly versions of Spotify's JSON payloads.

// Playlist is a user-owned Spotify playlist.
type Playlist struct {
	ID         string
	Name       string
	OwnerName  string
	TrackCount int
}

// Track is a single playable audio track.
type Track struct {
	ID       string
	URI      string
	Name     string
	Artist   string // first artist only; collaborations use the primary artist
	Album    string
	Duration time.Duration
}

// Album is a Spotify album or single.
type Album struct {
	ID          string
	URI         string
	Name        string
	Artist      string // primary artist
	ReleaseDate string // YYYY, YYYY-MM, or YYYY-MM-DD depending on precision
	TrackCount  int
}

// Artist is a Spotify artist entity.
type Artist struct {
	ID     string
	URI    string
	Name   string
	Genres []string
}

// Show is a podcast show.
type Show struct {
	ID            string
	URI           string
	Name          string
	TotalEpisodes int
}

// Episode is a single podcast episode.
type Episode struct {
	ID          string
	URI         string
	Name        string
	ReleaseDate string
	Duration    time.Duration
}

// Device is a Spotify Connect playback target.
type Device struct {
	ID     string
	Name   string
	Type   string // "Computer", "Smartphone", "Speaker", etc.
	Active bool
	Volume int // 0–100
}

// PlayerState is a snapshot of the user's current playback. TrackURI is
// empty when no item is playing; callers typically treat a nil *PlayerState
// from GetPlayerState as "nothing is playing".
type PlayerState struct {
	Playing       bool
	Shuffling     bool
	TrackName     string
	ArtistName    string // podcast shows use the show name here
	TrackURI      string
	ContextURI    string // playlist/album/show URI the track is being played from
	ImageURL      string // mid-size cover image URL
	ProgressMs    int    // playback position in milliseconds
	DurationMs    int    // total track length in milliseconds
	DeviceName    string
	VolumePercent int // 0–100; active device's volume (100 if device reports none)
}

// Raw JSON shapes from Spotify responses. Kept unexported — callers
// consume the flattened Track/Album/etc. types above.

type rawArtistRef struct {
	Name string `json:"name"`
}

func firstArtist(artists []rawArtistRef) string {
	if len(artists) > 0 {
		return artists[0].Name
	}
	return ""
}

type rawAlbum struct {
	ID          string         `json:"id"`
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	ReleaseDate string         `json:"release_date"`
	TotalTracks int            `json:"total_tracks"`
	Artists     []rawArtistRef `json:"artists"`
}

type rawTrack struct {
	ID       string         `json:"id"`
	URI      string         `json:"uri"`
	Name     string         `json:"name"`
	Duration int            `json:"duration_ms"`
	Artists  []rawArtistRef `json:"artists"`
	Album    struct {
		Name string `json:"name"`
	} `json:"album"`
}

type rawEpisode struct {
	ID          string `json:"id"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	DurationMs  int    `json:"duration_ms"`
}

type rawArtist struct {
	ID     string   `json:"id"`
	URI    string   `json:"uri"`
	Name   string   `json:"name"`
	Genres []string `json:"genres"`
}

type rawShow struct {
	ID            string `json:"id"`
	URI           string `json:"uri"`
	Name          string `json:"name"`
	TotalEpisodes int    `json:"total_episodes"`
}

// page is the common shape for Spotify paginated responses.
type page[T any] struct {
	Offset int `json:"offset"`
	Total  int `json:"total"`
	Items  []T `json:"items"`
}

func hasMore(offset, count, total int) bool {
	return offset+count < total
}

// Converters lift raw JSON rows into domain types.

func convertAlbums(raw []rawAlbum) []Album {
	var albums []Album
	for _, a := range raw {
		albums = append(albums, Album{
			ID:          a.ID,
			URI:         a.URI,
			Name:        a.Name,
			Artist:      firstArtist(a.Artists),
			ReleaseDate: a.ReleaseDate,
			TrackCount:  a.TotalTracks,
		})
	}
	return albums
}

func convertTracks(raw []rawTrack) []Track {
	var tracks []Track
	for _, t := range raw {
		tracks = append(tracks, Track{
			ID:       t.ID,
			URI:      t.URI,
			Name:     t.Name,
			Artist:   firstArtist(t.Artists),
			Album:    t.Album.Name,
			Duration: time.Duration(t.Duration) * time.Millisecond,
		})
	}
	return tracks
}

func convertEpisodes(raw []rawEpisode) []Episode {
	var episodes []Episode
	for _, e := range raw {
		episodes = append(episodes, Episode{
			ID:          e.ID,
			URI:         e.URI,
			Name:        e.Name,
			ReleaseDate: e.ReleaseDate,
			Duration:    time.Duration(e.DurationMs) * time.Millisecond,
		})
	}
	return episodes
}

func convertArtists(raw []rawArtist) []Artist {
	var artists []Artist
	for _, a := range raw {
		artists = append(artists, Artist{
			ID:     a.ID,
			URI:    a.URI,
			Name:   a.Name,
			Genres: a.Genres,
		})
	}
	return artists
}

func convertShows(raw []rawShow) []Show {
	var shows []Show
	for _, s := range raw {
		shows = append(shows, Show{
			ID:            s.ID,
			URI:           s.URI,
			Name:          s.Name,
			TotalEpisodes: s.TotalEpisodes,
		})
	}
	return shows
}
