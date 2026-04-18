package spotify

import (
	"context"
	"fmt"
)

// GetPlaylists returns the user's own playlists. The second return value (rawCount)
// is the unfiltered API page size, which callers must use to advance the offset
// (since it includes items filtered out by owner matching).
func (c *Client) GetPlaylists(ctx context.Context, offset, limit int) (playlists []Playlist, rawCount int, more bool, err error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/me/playlists?limit=%d&offset=%d", limit, offset)
	var page struct {
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Owner struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"owner"`
			Items struct {
				Total int `json:"total"`
			} `json:"items"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, 0, false, err
	}
	var result []Playlist
	for _, p := range page.Items {
		if c.userID != "" && p.Owner.ID != c.userID {
			continue
		}
		result = append(result, Playlist{
			ID:         p.ID,
			Name:       p.Name,
			OwnerName:  p.Owner.DisplayName,
			TrackCount: p.Items.Total,
		})
	}
	return result, len(page.Items), hasMore(page.Offset, len(page.Items), page.Total), nil
}

// GetPlaylistTracks returns tracks from a playlist, starting at offset.
// The bool indicates whether more pages are available.
func (c *Client) GetPlaylistTracks(ctx context.Context, id string, offset, limit int) ([]Track, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/items?limit=%d&offset=%d", id, limit, offset)
	var page struct {
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			Item rawTrack `json:"item"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, false, err
	}
	var raw []rawTrack
	for _, item := range page.Items {
		if item.Item.ID != "" {
			raw = append(raw, item.Item)
		}
	}
	return convertTracks(raw), hasMore(page.Offset, len(page.Items), page.Total), nil
}

// GetSavedShows returns the user's followed podcast shows.
func (c *Client) GetSavedShows(ctx context.Context, offset, limit int) ([]Show, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/me/shows?limit=%d&offset=%d", limit, offset)
	var p struct {
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			Show rawShow `json:"show"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &p); err != nil {
		return nil, false, err
	}
	raw := make([]rawShow, len(p.Items))
	for i, item := range p.Items {
		raw[i] = item.Show
	}
	return convertShows(raw), hasMore(p.Offset, len(p.Items), p.Total), nil
}

// GetShowEpisodes returns episodes for a given podcast show.
func (c *Client) GetShowEpisodes(ctx context.Context, showID string, offset, limit int) ([]Episode, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/shows/%s/episodes?limit=%d&offset=%d", showID, limit, offset)
	var p page[rawEpisode]
	if err := c.apiGet(ctx, url, &p); err != nil {
		return nil, false, err
	}
	return convertEpisodes(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}

// GetArtistAlbums returns albums and singles by the given artist.
// Compilation and appears-on releases are excluded.
func (c *Client) GetArtistAlbums(ctx context.Context, artistID string, offset, limit int) ([]Album, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/artists/%s/albums?include_groups=album,single&limit=%d&offset=%d",
		artistID, limit, offset)
	var p page[rawAlbum]
	if err := c.apiGet(ctx, endpoint, &p); err != nil {
		return nil, false, err
	}
	return convertAlbums(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}

// GetAlbumTracks returns tracks from the given album in track order.
func (c *Client) GetAlbumTracks(ctx context.Context, albumID string, offset, limit int) ([]Track, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/albums/%s/tracks?limit=%d&offset=%d",
		albumID, limit, offset)
	var p page[rawTrack]
	if err := c.apiGet(ctx, endpoint, &p); err != nil {
		return nil, false, err
	}
	return convertTracks(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}
