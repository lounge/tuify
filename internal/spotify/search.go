package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
)

// SearchTracks runs a track search against the Spotify catalog.
func (c *Client) SearchTracks(ctx context.Context, query string, offset, limit int) ([]Track, bool, error) {
	return search(ctx, c, query, "track", "tracks", offset, limit, convertTracks)
}

// SearchEpisodes runs a podcast-episode search against the Spotify catalog.
func (c *Client) SearchEpisodes(ctx context.Context, query string, offset, limit int) ([]Episode, bool, error) {
	return search(ctx, c, query, "episode", "episodes", offset, limit, convertEpisodes)
}

// SearchAlbums runs an album search against the Spotify catalog.
func (c *Client) SearchAlbums(ctx context.Context, query string, offset, limit int) ([]Album, bool, error) {
	return search(ctx, c, query, "album", "albums", offset, limit, convertAlbums)
}

// SearchArtists runs an artist search against the Spotify catalog.
func (c *Client) SearchArtists(ctx context.Context, query string, offset, limit int) ([]Artist, bool, error) {
	return search(ctx, c, query, "artist", "artists", offset, limit, convertArtists)
}

// SearchShows runs a podcast-show search against the Spotify catalog.
func (c *Client) SearchShows(ctx context.Context, query string, offset, limit int) ([]Show, bool, error) {
	return search(ctx, c, query, "show", "shows", offset, limit, convertShows)
}

// search performs a Spotify search API call and converts the results.
// searchType is the Spotify type parameter (e.g. "track"), key is the
// JSON response wrapper (e.g. "tracks").
func search[Raw, T any](ctx context.Context, c *Client, query, searchType, key string, offset, limit int, convert func([]Raw) []T) ([]T, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=%s&limit=%d&offset=%d",
		neturl.QueryEscape(query), searchType, limit, offset)
	var wrapper map[string]json.RawMessage
	if err := c.apiGet(ctx, endpoint, &wrapper); err != nil {
		return nil, false, err
	}
	raw, ok := wrapper[key]
	if !ok {
		return nil, false, fmt.Errorf("search response missing %q key", key)
	}
	var p page[Raw]
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, false, err
	}
	return convert(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}
