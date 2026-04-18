package spotify

import (
	"context"
	"encoding/json"
	"net/http"

	sp "github.com/zmb3/spotify/v2"
)

// GetPlayerState fetches the user's current playback state. Returns
// (nil, nil) when nothing is playing (HTTP 204) or when the active item
// is not a track/episode — callers should treat a nil *PlayerState as
// "no playback" rather than an error.
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
			Name          string `json:"name"`
			VolumePercent *int   `json:"volume_percent"` // nil when device reports no volume
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
		Playing:       state.Playing,
		Shuffling:     state.Shuffling,
		TrackName:     state.Item.Name,
		TrackURI:      state.Item.URI,
		ProgressMs:    state.ProgressMs,
		DurationMs:    state.Item.DurationMs,
		VolumePercent: 100,
	}
	if state.Device != nil {
		ps.DeviceName = state.Device.Name
		if state.Device.VolumePercent != nil {
			ps.VolumePercent = *state.Device.VolumePercent
		}
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

// Play starts playback of itemURI. If contextURI is set (playlist/album/
// show), the item plays in the context of that container so Next/Previous
// navigate within it; otherwise only the single item is queued.
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

// PlayQueue starts playback of an explicit list of track URIs in order.
// The first URI becomes the current item. Use Play when the items belong
// to a Spotify-side context (playlist/album) you want preserved.
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

// Resume resumes paused playback on the specified device.
func (c *Client) Resume(ctx context.Context, deviceID string) error {
	return c.sp.PlayOpt(ctx, playOpts(deviceID))
}

// Pause pauses playback on the specified device.
func (c *Client) Pause(ctx context.Context, deviceID string) error {
	return c.sp.PauseOpt(ctx, playOpts(deviceID))
}

// Stop pauses and seeks to the start of the current track. Spotify has no
// true "stop" — this is the closest approximation.
func (c *Client) Stop(ctx context.Context, deviceID string) error {
	opts := playOpts(deviceID)
	if err := c.sp.PauseOpt(ctx, opts); err != nil {
		return err
	}
	return c.sp.SeekOpt(ctx, 0, opts)
}

// Next skips to the next item in the current playback context.
func (c *Client) Next(ctx context.Context, deviceID string) error {
	return c.sp.NextOpt(ctx, playOpts(deviceID))
}

// Previous skips to the previous item, or restarts the current one if
// playback has advanced past the start (Spotify's native behavior).
func (c *Client) Previous(ctx context.Context, deviceID string) error {
	return c.sp.PreviousOpt(ctx, playOpts(deviceID))
}

// Shuffle enables or disables shuffle mode on the specified device.
func (c *Client) Shuffle(ctx context.Context, state bool, deviceID string) error {
	return c.sp.ShuffleOpt(ctx, state, playOpts(deviceID))
}

// Seek jumps to positionMs within the current track.
func (c *Client) Seek(ctx context.Context, positionMs int, deviceID string) error {
	return c.sp.SeekOpt(ctx, positionMs, playOpts(deviceID))
}

func playOpts(deviceID string) *sp.PlayOptions {
	opts := &sp.PlayOptions{}
	if deviceID != "" {
		id := sp.ID(deviceID)
		opts.DeviceID = &id
	}
	return opts
}
