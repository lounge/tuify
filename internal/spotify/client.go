package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"strconv"
	"time"

	sp "github.com/zmb3/spotify/v2"
)

type Client struct {
	sp     *sp.Client
	http   *http.Client
	userID string
}

type Playlist struct {
	ID         string
	Name       string
	OwnerName  string
	TrackCount int
}

type Track struct {
	ID       string
	URI      string
	Name     string
	Artist   string
	Album    string
	Duration time.Duration
}

type Album struct {
	ID          string
	URI         string
	Name        string
	Artist      string
	ReleaseDate string
	TrackCount  int
}

type Artist struct {
	ID     string
	URI    string
	Name   string
	Genres []string
}

type Show struct {
	ID            string
	URI           string
	Name          string
	TotalEpisodes int
}

type Episode struct {
	ID          string
	URI         string
	Name        string
	ReleaseDate string
	Duration    time.Duration
}

type PlayerState struct {
	Playing    bool
	Shuffling  bool
	TrackName  string
	ArtistName string
	TrackURI   string
	ProgressMs int
	DurationMs int
}

type rawAlbum struct {
	ID          string `json:"id"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	TotalTracks int    `json:"total_tracks"`
	Artists     []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

type rawTrack struct {
	ID       string `json:"id"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Duration int    `json:"duration_ms"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
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

func New(spClient *sp.Client, httpClient *http.Client) *Client {
	return &Client{sp: spClient, http: httpClient}
}

func (c *Client) FetchUserID(ctx context.Context) error {
	var me struct {
		ID string `json:"id"`
	}
	if err := c.apiGet(ctx, "https://api.spotify.com/v1/me", &me); err != nil {
		return err
	}
	c.userID = me.ID
	return nil
}

func (c *Client) GetPlaylists(ctx context.Context, offset, limit int) ([]Playlist, int, bool, error) {
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
	var playlists []Playlist
	for _, p := range page.Items {
		if c.userID != "" && p.Owner.ID != c.userID {
			continue
		}
		playlists = append(playlists, Playlist{
			ID:         p.ID,
			Name:       p.Name,
			OwnerName:  p.Owner.DisplayName,
			TrackCount: p.Items.Total,
		})
	}
	hasMore := page.Offset+len(page.Items) < page.Total
	return playlists, len(page.Items), hasMore, nil
}

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
	hasMore := page.Offset+len(page.Items) < page.Total
	return convertTracks(raw), hasMore, nil
}

func (c *Client) GetSavedShows(ctx context.Context, offset, limit int) ([]Show, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/me/shows?limit=%d&offset=%d", limit, offset)
	var page struct {
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			Show struct {
				ID            string `json:"id"`
				URI           string `json:"uri"`
				Name          string `json:"name"`
				TotalEpisodes int    `json:"total_episodes"`
			} `json:"show"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, false, err
	}
	var shows []Show
	for _, item := range page.Items {
		shows = append(shows, Show{
			ID:            item.Show.ID,
			URI:           item.Show.URI,
			Name:          item.Show.Name,
			TotalEpisodes: item.Show.TotalEpisodes,
		})
	}
	hasMore := page.Offset+len(page.Items) < page.Total
	return shows, hasMore, nil
}

func (c *Client) GetShowEpisodes(ctx context.Context, showID string, offset, limit int) ([]Episode, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/shows/%s/episodes?limit=%d&offset=%d", showID, limit, offset)
	var page struct {
		Offset int          `json:"offset"`
		Total  int          `json:"total"`
		Items  []rawEpisode `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, false, err
	}
	hasMore := page.Offset+len(page.Items) < page.Total
	return convertEpisodes(page.Items), hasMore, nil
}

func (c *Client) SearchTracks(ctx context.Context, query string, offset, limit int) ([]Track, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=track&limit=%d&offset=%d",
		neturl.QueryEscape(query), limit, offset)
	var result struct {
		Tracks struct {
			Offset int        `json:"offset"`
			Total  int        `json:"total"`
			Items  []rawTrack `json:"items"`
		} `json:"tracks"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	hasMore := result.Tracks.Offset+len(result.Tracks.Items) < result.Tracks.Total
	return convertTracks(result.Tracks.Items), hasMore, nil
}

func (c *Client) SearchEpisodes(ctx context.Context, query string, offset, limit int) ([]Episode, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=episode&limit=%d&offset=%d",
		neturl.QueryEscape(query), limit, offset)
	var result struct {
		Episodes struct {
			Offset int          `json:"offset"`
			Total  int          `json:"total"`
			Items  []rawEpisode `json:"items"`
		} `json:"episodes"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	hasMore := result.Episodes.Offset+len(result.Episodes.Items) < result.Episodes.Total
	return convertEpisodes(result.Episodes.Items), hasMore, nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, offset, limit int) ([]Album, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=album&limit=%d&offset=%d",
		neturl.QueryEscape(query), limit, offset)
	var result struct {
		Albums struct {
			Offset int        `json:"offset"`
			Total  int        `json:"total"`
			Items  []rawAlbum `json:"items"`
		} `json:"albums"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	hasMore := result.Albums.Offset+len(result.Albums.Items) < result.Albums.Total
	return convertAlbums(result.Albums.Items), hasMore, nil
}

func (c *Client) SearchArtists(ctx context.Context, query string, offset, limit int) ([]Artist, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=artist&limit=%d&offset=%d",
		neturl.QueryEscape(query), limit, offset)
	var result struct {
		Artists struct {
			Offset int `json:"offset"`
			Total  int `json:"total"`
			Items  []struct {
				ID     string   `json:"id"`
				URI    string   `json:"uri"`
				Name   string   `json:"name"`
				Genres []string `json:"genres"`
			} `json:"items"`
		} `json:"artists"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	var artists []Artist
	for _, a := range result.Artists.Items {
		artists = append(artists, Artist{
			ID:     a.ID,
			URI:    a.URI,
			Name:   a.Name,
			Genres: a.Genres,
		})
	}
	hasMore := result.Artists.Offset+len(result.Artists.Items) < result.Artists.Total
	return artists, hasMore, nil
}

func (c *Client) SearchShows(ctx context.Context, query string, offset, limit int) ([]Show, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/search?q=%s&type=show&limit=%d&offset=%d",
		neturl.QueryEscape(query), limit, offset)
	var result struct {
		Shows struct {
			Offset int `json:"offset"`
			Total  int `json:"total"`
			Items  []struct {
				ID            string `json:"id"`
				URI           string `json:"uri"`
				Name          string `json:"name"`
				TotalEpisodes int    `json:"total_episodes"`
			} `json:"items"`
		} `json:"shows"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	var shows []Show
	for _, s := range result.Shows.Items {
		shows = append(shows, Show{
			ID:            s.ID,
			URI:           s.URI,
			Name:          s.Name,
			TotalEpisodes: s.TotalEpisodes,
		})
	}
	hasMore := result.Shows.Offset+len(result.Shows.Items) < result.Shows.Total
	return shows, hasMore, nil
}

func (c *Client) GetArtistAlbums(ctx context.Context, artistID string, offset, limit int) ([]Album, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/artists/%s/albums?include_groups=album,single&limit=%d&offset=%d",
		artistID, limit, offset)
	var result struct {
		Offset int        `json:"offset"`
		Total  int        `json:"total"`
		Items  []rawAlbum `json:"items"`
	}
	if err := c.apiGet(ctx, endpoint, &result); err != nil {
		return nil, false, err
	}
	hasMore := result.Offset+len(result.Items) < result.Total
	return convertAlbums(result.Items), hasMore, nil
}

func (c *Client) GetAlbumTracks(ctx context.Context, albumID string, offset, limit int) ([]Track, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/albums/%s/tracks?limit=%d&offset=%d",
		albumID, limit, offset)
	var page struct {
		Offset int        `json:"offset"`
		Total  int        `json:"total"`
		Items  []rawTrack `json:"items"`
	}
	if err := c.apiGet(ctx, endpoint, &page); err != nil {
		return nil, false, err
	}
	hasMore := page.Offset+len(page.Items) < page.Total
	return convertTracks(page.Items), hasMore, nil
}

func (c *Client) GetPlayerState(ctx context.Context) (*PlayerState, error) {
	body, status, err := c.doWithRetry(ctx, "https://api.spotify.com/v1/me/player?additional_types=track,episode")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Spotify API %d: %s", status, body)
	}
	var state struct {
		Playing    bool `json:"is_playing"`
		Shuffling  bool `json:"shuffle_state"`
		ProgressMs int  `json:"progress_ms"`
		Item       *struct {
			Name       string `json:"name"`
			URI        string `json:"uri"`
			DurationMs int    `json:"duration_ms"`
			Artists    []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Show *struct {
				Name string `json:"name"`
			} `json:"show"`
		} `json:"item"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}
	if state.Item == nil {
		return nil, nil
	}
	ps := &PlayerState{
		Playing:    state.Playing,
		Shuffling:  state.Shuffling,
		TrackName:  state.Item.Name,
		TrackURI:   state.Item.URI,
		ProgressMs: state.ProgressMs,
		DurationMs: state.Item.DurationMs,
	}
	if len(state.Item.Artists) > 0 {
		ps.ArtistName = state.Item.Artists[0].Name
	} else if state.Item.Show != nil {
		ps.ArtistName = state.Item.Show.Name
	}
	return ps, nil
}

func (c *Client) Play(ctx context.Context, itemURI, contextURI, deviceID string) error {
	opts := playOpts(deviceID)
	if contextURI != "" {
		uri := sp.URI(contextURI)
		opts.PlaybackContext = &uri
		opts.PlaybackOffset = &sp.PlaybackOffset{URI: sp.URI(itemURI)}
	} else {
		opts.URIs = []sp.URI{sp.URI(itemURI)}
	}
	return c.sp.PlayOpt(ctx, opts)
}

func (c *Client) PlayQueue(ctx context.Context, uris []string, deviceID string) error {
	opts := playOpts(deviceID)
	for _, u := range uris {
		opts.URIs = append(opts.URIs, sp.URI(u))
	}
	if len(uris) > 0 {
		opts.PlaybackOffset = &sp.PlaybackOffset{URI: sp.URI(uris[0])}
	}
	return c.sp.PlayOpt(ctx, opts)
}

func (c *Client) Resume(ctx context.Context, deviceID string) error {
	return c.sp.PlayOpt(ctx, playOpts(deviceID))
}

func (c *Client) Pause(ctx context.Context, deviceID string) error {
	return c.sp.PauseOpt(ctx, playOpts(deviceID))
}

func (c *Client) Stop(ctx context.Context, deviceID string) error {
	opts := playOpts(deviceID)
	if err := c.sp.PauseOpt(ctx, opts); err != nil {
		return err
	}
	return c.sp.SeekOpt(ctx, 0, opts)
}

func (c *Client) Next(ctx context.Context, deviceID string) error {
	return c.sp.NextOpt(ctx, playOpts(deviceID))
}

func (c *Client) Previous(ctx context.Context, deviceID string) error {
	return c.sp.PreviousOpt(ctx, playOpts(deviceID))
}

func (c *Client) Shuffle(ctx context.Context, state bool, deviceID string) error {
	return c.sp.ShuffleOpt(ctx, state, playOpts(deviceID))
}

func (c *Client) Seek(ctx context.Context, positionMs int, deviceID string) error {
	return c.sp.SeekOpt(ctx, positionMs, playOpts(deviceID))
}

func (c *Client) FindDevice(ctx context.Context) (string, error) {
	devices, err := c.sp.PlayerDevices(ctx)
	if err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", fmt.Errorf("No Spotify devices found — open Spotify on any device")
	}
	for _, d := range devices {
		if d.Active {
			return string(d.ID), nil
		}
	}
	return string(devices[0].ID), nil
}

// doWithRetry performs a GET request with 429 retry logic. Returns the
// response body and status code, or an error if the request itself failed.
func (c *Client) doWithRetry(ctx context.Context, url string) ([]byte, int, error) {
	for attempts := 0; attempts < 3; attempts++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, err
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			log.Printf("[spotify] %s %d body=%s", url, resp.StatusCode, body)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			if wait > 10 {
				return nil, resp.StatusCode, fmt.Errorf("rate limited — retry in %dm", wait/60)
			}
			select {
			case <-time.After(time.Duration(wait) * time.Second):
				continue
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
		return body, resp.StatusCode, nil
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("Spotify API 429: rate limited after retries")
}

func (c *Client) apiGet(ctx context.Context, url string, result interface{}) error {
	body, status, err := c.doWithRetry(ctx, url)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("Spotify API %d: %s", status, body)
	}
	return json.Unmarshal(body, result)
}

func convertAlbums(raw []rawAlbum) []Album {
	var albums []Album
	for _, a := range raw {
		artist := ""
		if len(a.Artists) > 0 {
			artist = a.Artists[0].Name
		}
		albums = append(albums, Album{
			ID:          a.ID,
			URI:         a.URI,
			Name:        a.Name,
			Artist:      artist,
			ReleaseDate: a.ReleaseDate,
			TrackCount:  a.TotalTracks,
		})
	}
	return albums
}

func convertTracks(raw []rawTrack) []Track {
	var tracks []Track
	for _, t := range raw {
		artist := ""
		if len(t.Artists) > 0 {
			artist = t.Artists[0].Name
		}
		tracks = append(tracks, Track{
			ID:       t.ID,
			URI:      t.URI,
			Name:     t.Name,
			Artist:   artist,
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

func playOpts(deviceID string) *sp.PlayOptions {
	opts := &sp.PlayOptions{}
	if deviceID != "" {
		id := sp.ID(deviceID)
		opts.DeviceID = &id
	}
	return opts
}
