// Package uri holds pure string helpers for Spotify URIs
// (spotify:track:ID, spotify:episode:ID, spotify:playlist:ID, etc.).
// Stateless and import-free within the UI tree — safe to depend on from
// anywhere in ui/ without creating circular imports.
package uri

import "strings"

// IsPlayable reports whether uri points to a playable item (track or episode).
// Containers like playlists/albums/artists/shows are not playable directly.
func IsPlayable(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:") ||
		strings.HasPrefix(uri, "spotify:episode:")
}

// IsEpisode reports whether uri points to a podcast episode.
func IsEpisode(uri string) bool {
	return strings.HasPrefix(uri, "spotify:episode:")
}

// ID returns the trailing segment of uri (the bare Spotify ID) or the
// original string if no ':' separator is present.
func ID(uri string) string {
	if i := strings.LastIndex(uri, ":"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// URL converts a Spotify URI to its open.spotify.com web-player URL.
// Returns an empty string if uri is malformed.
func URL(uri string) string {
	parts := strings.SplitN(uri, ":", 3)
	if len(parts) == 3 {
		return "https://open.spotify.com/" + parts[1] + "/" + parts[2]
	}
	return ""
}
