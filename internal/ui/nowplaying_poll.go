package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m nowPlayingModel) tick() tea.Cmd {
	return tea.Tick(m.pollInterval(), func(t time.Time) tea.Msg {
		return nowPlayingTickMsg(t)
	})
}

func (m nowPlayingModel) progressTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
	})
}

func (m nowPlayingModel) labelScrollTick() tea.Cmd {
	return tea.Tick(labelScrollInterval, func(t time.Time) tea.Msg {
		return labelScrollMsg(t)
	})
}

func (m nowPlayingModel) pollInterval() time.Duration {
	// During a rate-limit cooldown, schedule the next tick to land just
	// past the deadline so we resume polling immediately when the gate
	// opens — instead of ticking every 10s through the dead window.
	if wait := m.client.RateLimitWait(); wait > 0 {
		return wait + time.Second
	}
	if !m.hasTrack {
		return 10 * time.Second
	}
	if time.Since(m.lastUserAction) < 30*time.Second {
		return 5 * time.Second
	}
	if !m.playing {
		return 15 * time.Second
	}
	if m.durationMs-m.progressMs < nearEndThresholdMs {
		return 3 * time.Second
	}
	return 10 * time.Second
}

func (m nowPlayingModel) pollState() tea.Cmd {
	client := m.client
	parent := m.ctx
	return func() tea.Msg {
		// Skip the API call entirely during cooldown; the tick is already
		// rescheduled for after the deadline. Returning a sentinel skipped
		// message keeps handlePlayerState from clearing hasTrack.
		if client.IsRateLimited() {
			return playerStateMsg{skipped: true}
		}
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		state, err := client.GetPlayerState(ctx)
		return playerStateMsg{state: state, err: err}
	}
}

func (m *nowPlayingModel) recordUserAction() {
	m.lastUserAction = time.Now()
}
