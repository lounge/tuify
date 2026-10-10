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
	wasShuffling := m.nowPlaying.shuffling
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if err := c.PlayQueue(ctx, uris, id); err != nil {
			return err
		}
		restoreShuffle(ctx, c, id, wasShuffling)
		return nil
	}, opPlayback)
}

func (m Model) playItem(itemURI, contextURI string) tea.Cmd {
	wasShuffling := m.nowPlaying.shuffling
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if err := c.Play(ctx, itemURI, contextURI, id); err != nil {
			return err
		}
		restoreShuffle(ctx, c, id, wasShuffling)
		return nil
	}, opPlayback)
}

// restoreShuffle re-applies the user's shuffle state after a manual track
// selection. Spotify's PUT /v1/me/player/play with an explicit offset or
// URI list turns shuffle off (unlike Next/Previous which preserve it), so
// without this the shuffle indicator would disappear every time the user
// clicked a track. Errors are logged, not surfaced — playback already
// started successfully, and the next GetPlayerState poll will reconcile.
func restoreShuffle(ctx context.Context, c *spotify.Client, deviceID string, wasShuffling bool) {
	if !wasShuffling {
		return
	}
	if err := c.Shuffle(ctx, true, deviceID); err != nil {
		log.Printf("[playback] failed to restore shuffle state: %v", err)
	}
}

// togglePlayPause pauses or resumes. flip is the number of the optimistic
// flip the key handler made (nowPlayingModel.beginFlip); it rides on the
// reply so a failure reverts that flip and no later one.
func (m Model) togglePlayPause(wasPlaying bool, flip uint64) tea.Cmd {
	return m.withDeviceFlip(func(ctx context.Context, c *spotify.Client, id string) error {
		if wasPlaying {
			return c.Pause(ctx, id)
		}
		return c.Resume(ctx, id)
	}, opPlayPause, flip)
}

func (m Model) nextTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Next(ctx, id)
	}, opPlayback)
}

func (m Model) previousTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Previous(ctx, id)
	}, opPlayback)
}

// toggleShuffle sets shuffle. flip is as for togglePlayPause.
func (m Model) toggleShuffle(newState bool, flip uint64) tea.Cmd {
	return m.withDeviceFlip(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Shuffle(ctx, newState, id)
	}, opShuffle, flip)
}

func (m Model) stopPlayback() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Stop(ctx, id)
	}, opPlayback)
}

// seekRelative moves the position by deltaMs and schedules the seek after
// a short debounce so a burst of key presses sends one request. With no
// track reported there is nothing to seek in: the key is a no-op rather
// than a request that 404s into a banner.
func (m *Model) seekRelative(deltaMs int) tea.Cmd {
	if !m.nowPlaying.hasTrack {
		return nil
	}
	posMs := min(max(m.nowPlaying.progressMs+deltaMs, 0), m.nowPlaying.durationMs)
	m.nowPlaying.progressMs = posMs
	m.nowPlaying.seekPending = true
	// The user chose a position; a cached episode position the guard was
	// still waiting for is superseded, and polls must show the real one.
	m.nowPlaying.resumeUntilMs = 0
	m.seekSeq++
	seq, trackURI := m.seekSeq, m.nowPlaying.trackURI
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return seekFireMsg{seq: seq, posMs: posMs, trackURI: trackURI}
	})
}

func (m Model) copyTrackLink() tea.Cmd {
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
	return transferDeviceCmd(m.rootCtx, m.client, dev, m.deviceSelector.activeDeviceID, m.nowPlaying.progressMs, m.nowPlaying.playing)
}

// withDevice wraps a Spotify API call with device resolution. If the user has
// manually switched devices, it targets the active one; otherwise it prefers
// the configured device and re-establishes playback if the preferred device
// is present but inactive (e.g. librespot idle after a pause). op tags the
// result so handlePlaybackResult knows which command it answers.
func (m Model) withDevice(fn func(ctx context.Context, client *spotify.Client, deviceID string) error, op playbackOp) tea.Cmd {
	return m.withDeviceFlip(fn, op, 0)
}

// withDeviceFlip is withDevice for a command that flipped now-playing state
// ahead of its reply; flip is that flip's number and is carried back on the
// playbackResultMsg so a failure reverts it and no other.
func (m Model) withDeviceFlip(fn func(ctx context.Context, client *spotify.Client, deviceID string) error, op playbackOp, flip uint64) tea.Cmd {
	client := m.client
	parent := m.rootCtx
	trackURI := m.nowPlaying.trackURI
	contextURI := m.nowPlaying.contextURI
	return func() tea.Msg {
		// If the user manually switched to another device in Spotify,
		// target whatever device is currently active instead of re-claiming.
		if client.DeviceOverridden.Load() {
			ctx, cancel := context.WithTimeout(parent, 10*time.Second)
			defer cancel()
			log.Printf("[withDevice] DeviceOverridden=true, finding active device")
			deviceID, _, _, err := client.FindDevice(ctx, true)
			if err != nil {
				return playbackResultMsg{err: err, op: op, flip: flip}
			}
			log.Printf("[withDevice] targeting overridden device: %s", deviceID)
			return playbackResultMsg{err: fn(ctx, client, deviceID), op: op, flip: flip}
		}

		findCtx, findCancel := context.WithTimeout(parent, 10*time.Second)
		deviceID, active, preferred, err := client.FindDevice(findCtx, false)
		findCancel()
		if err != nil {
			return playbackResultMsg{err: err, op: op, flip: flip}
		}
		log.Printf("[withDevice] device=%s active=%v preferred=%v overridden=%v", deviceID, active, preferred, client.DeviceOverridden.Load())
		// Re-establish playback (resume inside the current context) only
		// when the preferred device was found but is inactive (e.g.
		// librespot idle). Otherwise deviceID is the active device, or,
		// when nothing is active and the preferred device is not listed,
		// FindDevice's fallback. The fallback is only ever reached when no
		// device is active, so there is no playback to steal; fn runs
		// against it as is, and a Play with its device_id does start
		// playback there. The re-establishment uses its own short budget so
		// fn below gets a fresh deadline regardless of how long device
		// wake-up takes.
		if !active && preferred {
			reCtx, reCancel := context.WithTimeout(parent, 5*time.Second)
			var transferErr error
			if contextURI != "" && trackURI != "" {
				transferErr = client.Play(reCtx, trackURI, contextURI, deviceID)
			} else {
				transferErr = client.TransferPlayback(reCtx, deviceID, true)
			}
			reCancel()
			if transferErr != nil {
				log.Printf("[playback] device re-establishment failed: %v", transferErr)
			}
		}

		fnCtx, fnCancel := context.WithTimeout(parent, 10*time.Second)
		defer fnCancel()
		return playbackResultMsg{err: fn(fnCtx, client, deviceID), op: op, flip: flip}
	}
}
