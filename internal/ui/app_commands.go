package ui

import (
	"context"
	"log"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// Playback commands — each returns a tea.Cmd that performs a Spotify API
// call through withDevice, which handles device resolution and the
// user-overridden-device case.

func (m Model) playQueue(uris []string) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.PlayQueue(ctx, uris, id)
	}, false)
}

func (m Model) playItem(itemURI, contextURI string) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Play(ctx, itemURI, contextURI, id)
	}, false)
}

func (m Model) togglePlayPause(wasPlaying bool) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if wasPlaying {
			return c.Pause(ctx, id)
		}
		return c.Resume(ctx, id)
	}, false)
}

func (m Model) nextTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Next(ctx, id)
	}, false)
}

func (m Model) previousTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Previous(ctx, id)
	}, false)
}

func (m Model) toggleShuffle(newState bool) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Shuffle(ctx, newState, id)
	}, false)
}

func (m Model) stopPlayback() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Stop(ctx, id)
	}, false)
}

func (m *Model) seekRelative(deltaMs int) tea.Cmd {
	posMs := m.nowPlaying.progressMs + deltaMs
	if posMs < 0 {
		posMs = 0
	}
	if posMs > m.nowPlaying.durationMs {
		posMs = m.nowPlaying.durationMs
	}
	m.nowPlaying.progressMs = posMs
	m.nowPlaying.seekPending = true
	m.seekSeq++
	seq := m.seekSeq
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return seekFireMsg{seq: seq, posMs: posMs}
	})
}

func (m *Model) copyTrackLink() tea.Cmd {
	if !m.nowPlaying.hasTrack {
		return nil
	}
	url := spotifyURL(m.nowPlaying.trackURI)
	if url == "" {
		return nil
	}
	return func() tea.Msg {
		return clipboardResultMsg{err: clipboard.WriteAll(url)}
	}
}

func (m Model) transferDevice(dev spotify.Device) tea.Cmd {
	return transferDeviceCmd(m.client, dev, m.deviceSelector.activeDeviceID, m.nowPlaying.progressMs, m.nowPlaying.playing)
}

// withDevice wraps a Spotify API call with device resolution. If the user has
// manually switched devices, it targets the active one; otherwise it prefers
// the configured device and re-establishes playback if the preferred device
// is present but inactive (e.g. librespot idle after a pause).
func (m Model) withDevice(fn func(ctx context.Context, client *spotify.Client, deviceID string) error, seek bool) tea.Cmd {
	client := m.client
	trackURI := m.nowPlaying.trackURI
	contextURI := m.nowPlaying.contextURI
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// If the user manually switched to another device in Spotify,
		// target whatever device is currently active instead of re-claiming.
		if client.DeviceOverridden.Load() {
			log.Printf("[withDevice] DeviceOverridden=true, finding active device")
			deviceID, _, _, err := client.FindDevice(ctx, true)
			if err != nil {
				log.Printf("[withDevice] FindDevice(activeOnly) failed: %v", err)
				return playbackResultMsg{err: err, seek: seek}
			}
			log.Printf("[withDevice] targeting overridden device: %s", deviceID)
			if err := fn(ctx, client, deviceID); err != nil {
				log.Printf("[withDevice] command failed on overridden device: %v", err)
				return playbackResultMsg{err: err, seek: seek}
			}
			return playbackResultMsg{err: nil, seek: seek}
		}

		deviceID, active, preferred, err := client.FindDevice(ctx, false)
		if err != nil {
			log.Printf("[withDevice] FindDevice failed: %v", err)
			return playbackResultMsg{err: err, seek: seek}
		}
		log.Printf("[withDevice] device=%s active=%v preferred=%v overridden=%v", deviceID, active, preferred, client.DeviceOverridden.Load())
		// Re-establish playback only when the preferred device was found but is
		// inactive (e.g. librespot idle). If the preferred device is missing
		// from the API response entirely (flaky API), don't transfer to a
		// fallback device — that would steal playback from the actual player.
		if !active && preferred {
			var transferErr error
			if contextURI != "" && trackURI != "" {
				transferErr = client.Play(ctx, trackURI, contextURI, deviceID)
			} else {
				transferErr = client.TransferPlayback(ctx, deviceID, true)
			}
			if transferErr != nil {
				log.Printf("[playback] device re-establishment failed: %v", transferErr)
			}
		}
		return playbackResultMsg{err: fn(ctx, client, deviceID), seek: seek}
	}
}
