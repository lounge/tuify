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
	"sync/atomic"
	"time"

	sp "github.com/zmb3/spotify/v2"
)

type Client struct {
	sp              *sp.Client
	httpClient      *http.Client
	userID          string
	PreferredDevice string // if set, FindDevice prefers this device name

	// DeviceOverridden is set when the user manually switches playback to
	// another device in Spotify. Checked by the librespot OnReconnect
	// callback to avoid stealing playback back.
	DeviceOverridden atomic.Bool
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

type Device struct {
	ID     string
	Name   string
	Type   string // "Computer", "Smartphone", "Speaker", etc.
	Active bool
	Volume int // 0–100
}

type PlayerState struct {
	Playing    bool
	Shuffling  bool
	TrackName  string
	ArtistName string
	TrackURI   string
	ContextURI string
	ImageURL   string
	ProgressMs int
	DurationMs int
	DeviceName string
}

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

func New(spClient *sp.Client, httpClient *http.Client) *Client {
	return &Client{sp: spClient, httpClient: httpClient}
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

func (c *Client) GetShowEpisodes(ctx context.Context, showID string, offset, limit int) ([]Episode, bool, error) {
	url := fmt.Sprintf("https://api.spotify.com/v1/shows/%s/episodes?limit=%d&offset=%d", showID, limit, offset)
	var p page[rawEpisode]
	if err := c.apiGet(ctx, url, &p); err != nil {
		return nil, false, err
	}
	return convertEpisodes(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}

func (c *Client) SearchTracks(ctx context.Context, query string, offset, limit int) ([]Track, bool, error) {
	return search(ctx, c, query, "track", "tracks", offset, limit, convertTracks)
}

func (c *Client) SearchEpisodes(ctx context.Context, query string, offset, limit int) ([]Episode, bool, error) {
	return search(ctx, c, query, "episode", "episodes", offset, limit, convertEpisodes)
}

func (c *Client) SearchAlbums(ctx context.Context, query string, offset, limit int) ([]Album, bool, error) {
	return search(ctx, c, query, "album", "albums", offset, limit, convertAlbums)
}

func (c *Client) SearchArtists(ctx context.Context, query string, offset, limit int) ([]Artist, bool, error) {
	return search(ctx, c, query, "artist", "artists", offset, limit, convertArtists)
}

func (c *Client) SearchShows(ctx context.Context, query string, offset, limit int) ([]Show, bool, error) {
	return search(ctx, c, query, "show", "shows", offset, limit, convertShows)
}

func (c *Client) GetArtistAlbums(ctx context.Context, artistID string, offset, limit int) ([]Album, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/artists/%s/albums?include_groups=album,single&limit=%d&offset=%d",
		artistID, limit, offset)
	var p page[rawAlbum]
	if err := c.apiGet(ctx, endpoint, &p); err != nil {
		return nil, false, err
	}
	return convertAlbums(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}

func (c *Client) GetAlbumTracks(ctx context.Context, albumID string, offset, limit int) ([]Track, bool, error) {
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/albums/%s/tracks?limit=%d&offset=%d",
		albumID, limit, offset)
	var p page[rawTrack]
	if err := c.apiGet(ctx, endpoint, &p); err != nil {
		return nil, false, err
	}
	return convertTracks(p.Items), hasMore(p.Offset, len(p.Items), p.Total), nil
}

func (c *Client) GetPlayerState(ctx context.Context) (*PlayerState, error) {
	body, status, err := c.doWithRetry(ctx, "https://api.spotify.com/v1/me/player?additional_types=track,episode")
	if status == http.StatusNoContent {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state struct {
		Playing    bool `json:"is_playing"`
		Shuffling  bool `json:"shuffle_state"`
		ProgressMs int  `json:"progress_ms"`
		Device     *struct {
			Name string `json:"name"`
		} `json:"device"`
		Context *struct {
			URI string `json:"uri"`
		} `json:"context"`
		Item *struct {
			Name       string `json:"name"`
			URI        string `json:"uri"`
			DurationMs int    `json:"duration_ms"`
			Artists    []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Show *struct {
				Name string `json:"name"`
			} `json:"show"`
			Album *struct {
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"album"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
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
	if state.Device != nil {
		ps.DeviceName = state.Device.Name
	}
	if state.Context != nil {
		ps.ContextURI = state.Context.URI
	}
	if len(state.Item.Artists) > 0 {
		ps.ArtistName = state.Item.Artists[0].Name
	} else if state.Item.Show != nil {
		ps.ArtistName = state.Item.Show.Name
	}
	if state.Item.Album != nil && len(state.Item.Album.Images) > 0 {
		images := state.Item.Album.Images
		ps.ImageURL = images[len(images)/2].URL
	} else if len(state.Item.Images) > 0 {
		images := state.Item.Images
		ps.ImageURL = images[len(images)/2].URL
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

func (c *Client) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	return c.sp.TransferPlayback(ctx, sp.ID(deviceID), play)
}

// GetDevices returns all available Spotify Connect devices.
func (c *Client) GetDevices(ctx context.Context) ([]Device, error) {
	devices, err := c.sp.PlayerDevices(ctx)
	if err != nil {
		log.Printf("[devices] GetDevices API error: %v", err)
		return nil, err
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, Device{
			ID:     string(d.ID),
			Name:   d.Name,
			Type:   d.Type,
			Active: d.Active,
			Volume: int(d.Volume),
		})
	}
	return out, nil
}

// FindDevice returns the best device ID, whether it is currently active, and
// whether the returned device is the configured preferred device.
// When activeOnly is true, only a device currently marked active by Spotify is
// returned; an error is returned if no device is active.
func (c *Client) FindDevice(ctx context.Context, activeOnly bool) (id string, active bool, preferred bool, err error) {
	devices, err := c.sp.PlayerDevices(ctx)
	if err != nil {
		return "", false, false, err
	}
	if len(devices) == 0 {
		return "", false, false, fmt.Errorf("no Spotify devices found — open Spotify on any device")
	}
	// When not restricted to active-only, prefer the configured device.
	if !activeOnly && c.PreferredDevice != "" {
		for _, d := range devices {
			if d.Name == c.PreferredDevice {
				return string(d.ID), d.Active, true, nil
			}
		}
	}
	for _, d := range devices {
		if d.Active {
			return string(d.ID), true, false, nil
		}
	}
	if activeOnly {
		return "", false, false, fmt.Errorf("no active Spotify device found")
	}
	return string(devices[0].ID), false, false, nil
}

// APIError is returned by doWithRetry for non-2xx responses. It carries the
// status code and (truncated) response body so callers can distinguish error
// shapes (e.g. StatusNoContent for "no active playback") without re-parsing.
type APIError struct {
	Status int
	Body   []byte
	URL    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Spotify API %d: %s", e.Status, e.Body)
}

// doWithRetry performs a GET request with 429 retry logic. Returns the
// response body, status code, and an error for non-2xx responses. The
// error is an *APIError for HTTP-level failures; callers that need to
// treat a specific status as non-error (e.g. 204) should check for it
// via errors.As before propagating.
func (c *Client) doWithRetry(ctx context.Context, url string) ([]byte, int, error) {
	for attempts := 0; attempts < 3; attempts++ {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := 0
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					wait = n
				}
			}
			if wait > 10 {
				return nil, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), URL: url}
			}
			select {
			case <-time.After(time.Duration(wait) * time.Second):
				continue
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, resp.StatusCode, nil
		}
		log.Printf("[spotify] %s %d body=%s", url, resp.StatusCode, truncateForLog(body))
		return body, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: truncateForLog(body), URL: url}
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("Spotify API 429: rate limited after retries")
}

// truncateForLog caps a response body for logging/error storage. Large
// bodies can contain sensitive tokens or flood logs.
func truncateForLog(b []byte) []byte {
	const max = 500
	if len(b) <= max {
		return b
	}
	return append(append([]byte(nil), b[:max]...), "…"...)
}

func (c *Client) apiGet(ctx context.Context, url string, result any) error {
	body, _, err := c.doWithRetry(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, result)
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

func playOpts(deviceID string) *sp.PlayOptions {
	opts := &sp.PlayOptions{}
	if deviceID != "" {
		id := sp.ID(deviceID)
		opts.DeviceID = &id
	}
	return opts
}
