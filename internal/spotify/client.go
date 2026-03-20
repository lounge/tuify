package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

type Show struct {
	ID            string
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
			ID   string `json:"id"`
			Name string `json:"name"`
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
			Item struct {
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
			} `json:"item"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, false, err
	}
	var tracks []Track
	for _, item := range page.Items {
		t := item.Item
		if t.ID == "" {
			continue
		}
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
	hasMore := page.Offset+len(page.Items) < page.Total
	return tracks, hasMore, nil
}

func (c *Client) GetSavedShows(ctx context.Context, offset, limit int) ([]Show, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/me/shows?limit=%d&offset=%d", limit, offset)
	var page struct {
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			Show struct {
				ID            string `json:"id"`
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
		Offset int `json:"offset"`
		Total  int `json:"total"`
		Items  []struct {
			ID          string `json:"id"`
			URI         string `json:"uri"`
			Name        string `json:"name"`
			ReleaseDate string `json:"release_date"`
			DurationMs  int    `json:"duration_ms"`
		} `json:"items"`
	}
	if err := c.apiGet(ctx, url, &page); err != nil {
		return nil, false, err
	}
	var episodes []Episode
	for _, e := range page.Items {
		episodes = append(episodes, Episode{
			ID:          e.ID,
			URI:         e.URI,
			Name:        e.Name,
			ReleaseDate: e.ReleaseDate,
			Duration:    time.Duration(e.DurationMs) * time.Millisecond,
		})
	}
	hasMore := page.Offset+len(page.Items) < page.Total
	return episodes, hasMore, nil
}

func (c *Client) apiGet(ctx context.Context, url string, result interface{}) error {
	for attempts := 0; attempts < 3; attempts++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		log.Printf("[spotify] %s %d body=%s", url, resp.StatusCode, body)
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			if wait > 10 {
				return fmt.Errorf("rate limited — retry in %dm", wait/60)
			}
			select {
			case <-time.After(time.Duration(wait) * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Spotify API %d: %s", resp.StatusCode, body)
		}
		return json.Unmarshal(body, result)
	}
	return fmt.Errorf("Spotify API 429: rate limited after retries")
}

func playOpts(deviceID string) *sp.PlayOptions {
	opts := &sp.PlayOptions{}
	if deviceID != "" {
		id := sp.ID(deviceID)
		opts.DeviceID = &id
	}
	return opts
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

func (c *Client) GetPlayerState(ctx context.Context) (*PlayerState, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.spotify.com/v1/me/player?additional_types=track,episode", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Spotify API %d: %s", resp.StatusCode, body)
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
