package spotify

import (
	"context"
	"fmt"
	"log"

	sp "github.com/zmb3/spotify/v2"
)

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

// TransferPlayback moves active playback to the given device. If play is
// true, playback resumes on the target; otherwise the target is primed
// but left in its current paused/playing state.
func (c *Client) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	return c.sp.TransferPlayback(ctx, sp.ID(deviceID), play)
}
