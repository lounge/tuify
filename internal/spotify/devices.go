package spotify

import (
	"context"
	"fmt"

	"github.com/lounge/tuify/internal/termsafe"
	sp "github.com/zmb3/spotify/v2"
)

// GetDevices returns all available Spotify Connect devices.
func (c *Client) GetDevices(ctx context.Context) ([]Device, error) {
	devices, err := c.sp.PlayerDevices(ctx)
	if err != nil {
		return nil, wrapSDKErr(err, opDevices)
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		out = append(out, Device{
			ID:     string(d.ID),
			Name:   termsafe.Clean(d.Name),
			Type:   termsafe.Clean(d.Type),
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
//
// A device Spotify lists without an ID is never returned, since no command
// can target it. The last-resort pick (no preferred match, nothing active)
// also skips restricted devices, which accept no Web API commands at all;
// when nothing else is listed the error says so.
func (c *Client) FindDevice(ctx context.Context, activeOnly bool) (id string, active bool, preferred bool, err error) {
	devices, err := c.sp.PlayerDevices(ctx)
	if err != nil {
		return "", false, false, wrapSDKErr(err, opDevices)
	}
	if len(devices) == 0 {
		return "", false, false, fmt.Errorf("no Spotify devices found — open Spotify on any device")
	}
	// When not restricted to active-only, prefer the configured device.
	if !activeOnly && c.preferredDevice != "" {
		for _, d := range devices {
			if d.Name == c.preferredDevice && d.ID != "" {
				return string(d.ID), d.Active, true, nil
			}
		}
	}
	for _, d := range devices {
		if d.Active && d.ID != "" {
			return string(d.ID), true, false, nil
		}
	}
	if activeOnly {
		return "", false, false, fmt.Errorf("no active Spotify device found")
	}
	for _, d := range devices {
		if d.ID != "" && !d.Restricted {
			return string(d.ID), false, false, nil
		}
	}
	return "", false, false, fmt.Errorf("no controllable Spotify device found — open Spotify on any device")
}

// TransferPlayback moves active playback to the given device. If play is
// true, playback resumes on the target; otherwise the target is primed
// but left in its current paused/playing state.
func (c *Client) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	return wrapSDKErr(c.sp.TransferPlayback(ctx, sp.ID(deviceID), play), opTransfer)
}
