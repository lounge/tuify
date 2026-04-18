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
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		state, err := client.GetPlayerState(ctx)
		return playerStateMsg{state: state, err: err}
	}
}

func (m *nowPlayingModel) recordUserAction() {
	m.lastUserAction = time.Now()
}
