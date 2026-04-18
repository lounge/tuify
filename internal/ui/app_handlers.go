package ui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
	zone "github.com/lrstanley/bubblezone"
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

// handleMouse routes a mouse event. Scroll wheel moves the current list's
// cursor (bubbles/list has no native mouse handling, so once we enable
// WithMouseCellMotion we're responsible for wheel translation). Left-click
// selects the zoned item under the cursor; a second left-click on the same
// item within doubleClickWindow fires the enter action (play / drill-down).
// Returns handled=false for any event that doesn't match a handled case —
// the caller lets those fall through to the regular Update path.
func (m Model) handleMouse(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return false, m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if time.Since(m.lastWheelTime) < wheelDebounceWindow {
			return true, m, nil
		}
		if l := m.currentList(); l != nil {
			l.CursorUp()
			m.lastWheelTime = time.Now()
			return true, m, nil
		}
		if hv, ok := m.currentView().(*homeView); ok {
			if hv.cursor > 0 {
				hv.cursor--
			}
			m.lastWheelTime = time.Now()
			return true, m, nil
		}
	case tea.MouseButtonWheelDown:
		if time.Since(m.lastWheelTime) < wheelDebounceWindow {
			return true, m, nil
		}
		if l := m.currentList(); l != nil {
			l.CursorDown()
			m.lastWheelTime = time.Now()
			return true, m, nil
		}
		if hv, ok := m.currentView().(*homeView); ok {
			if hv.cursor < len(homeItems)-1 {
				hv.cursor++
			}
			m.lastWheelTime = time.Now()
			return true, m, nil
		}
	case tea.MouseButtonLeft:
		return m.handleMouseClick(msg)
	}
	return false, m, nil
}

// handleMouseClick resolves a left-press against zones marked on the current
// view's clickable items. Double-click within doubleClickWindow fires Enter.
// Handles both list-backed views (tracks, playlists, etc.) and the home
// view's menu tabs.
func (m Model) handleMouseClick(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	if l := m.currentList(); l != nil {
		for i, item := range l.Items() {
			u, ok := item.(uriItem)
			if !ok || u.URI() == "" {
				continue
			}
			if !zone.Get(u.URI()).InBounds(msg) {
				continue
			}
			l.Select(i)
			return m.registerClick(u.URI())
		}
		return false, m, nil
	}

	if hv, ok := m.currentView().(*homeView); ok {
		for i, item := range homeItems {
			if !zone.Get(item.name).InBounds(msg) {
				continue
			}
			hv.cursor = i
			return m.registerClick(item.name)
		}
	}
	return false, m, nil
}

// registerClick records a click for double-click detection and fires
// handleEnter if this click closes a double-click pair on the same id.
func (m Model) registerClick(id string) (handled bool, model tea.Model, cmd tea.Cmd) {
	now := time.Now()
	if m.lastClickURI == id && now.Sub(m.lastClickTime) < doubleClickWindow {
		m.lastClickURI = ""
		m.lastClickTime = time.Time{}
		nm, c := m.handleEnter()
		return true, nm, c
	}
	m.lastClickURI = id
	m.lastClickTime = now
	return true, m, nil
}
