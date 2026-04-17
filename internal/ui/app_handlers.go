package ui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// Message handlers for non-key messages routed from Update. Each returns a
// (tea.Model, tea.Cmd) pair so Update can forward them directly.

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.nowPlaying.width = msg.Width
	for _, v := range m.viewStack {
		h := m.height - nowPlayingHeight
		if v.Breadcrumb() != "" {
			h -= breadcrumbHeight
		}
		v.SetSize(msg.Width, h)
	}
	return m, nil
}

func (m Model) handlePlaybackResult(msg playbackResultMsg) (tea.Model, tea.Cmd) {
	if msg.seek {
		m.nowPlaying.seekPending = false
	}
	if msg.err != nil {
		if m.nowPlaying.playPausePending {
			m.nowPlaying.playPausePending = false
			m.nowPlaying.playing = !m.nowPlaying.playing
		}
		if m.nowPlaying.shufflePending {
			m.nowPlaying.shufflePending = false
			m.nowPlaying.shuffling = !m.nowPlaying.shuffling
		}
		// Don't show transient network errors in the UI — they recover on their own.
		if errors.Is(msg.err, context.DeadlineExceeded) {
			if msg.seek {
				return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
			}
			return m, nil
		}
		errCmd := m.nowPlaying.SetError(msg.err.Error())
		if msg.seek {
			return m, tea.Batch(
				errCmd,
				tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
			)
		}
		return m, errCmd
	}
	if msg.seek {
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
	}
	// Staggered polls to catch the update once the API reflects the change.
	return m, tea.Batch(
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
	)
}

func (m Model) handleVizTick() (tea.Model, tea.Cmd) {
	if m.visualizer.active {
		m.visualizer.advance(m.nowPlaying.progressMs)
		return m, m.visualizer.tick()
	}
	return m, nil
}

func (m Model) handleEpisodeResume(msg episodeResumeMsg) (tea.Model, tea.Cmd) {
	posMs := msg.posMs
	return m, m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	}, true)
}

func (m Model) handleSeekFire(msg seekFireMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seekSeq {
		return m, nil // outdated, a newer seek superseded this one
	}
	posMs := msg.posMs
	return m, m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	}, true)
}
