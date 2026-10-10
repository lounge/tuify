package ui

import (
	"context"
	"errors"
	"log"
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
		if v.breadcrumb() != "" {
			h -= breadcrumbHeight
		}
		v.SetSize(msg.Width, h)
	}
	return m, nil
}

func (m Model) handlePlaybackResult(msg playbackResultMsg) (tea.Model, tea.Cmd) {
	seek := msg.op == opSeek
	if seek {
		m.nowPlaying.seekPending = false
	}
	if msg.err != nil {
		log.Printf("[playback] command failed: %v", msg.err)
		// Revert only the optimistic flip made for this command: the one
		// whose number the reply carries. A flip pending for another
		// command (a pause that succeeded while this Next failed) or a
		// later press of the same key (its own reply is still out) stays
		// until its reply or the next poll settles it.
		switch msg.op {
		case opPlayPause:
			if msg.flip != 0 && m.nowPlaying.playPausePending == msg.flip {
				m.nowPlaying.playPausePending = 0
				m.nowPlaying.playing = !m.nowPlaying.playing
				// The command never took effect, so neither did the
				// intent it recorded.
				m.client.SetPlayIntent(m.nowPlaying.playing)
			}
		case opShuffle:
			if msg.flip != 0 && m.nowPlaying.shufflePending == msg.flip {
				m.nowPlaying.shufflePending = 0
				m.nowPlaying.shuffling = !m.nowPlaying.shuffling
			}
		case opSeek:
			// A failed seek leaves the player where it was. If it was the
			// resume seek for a cached episode position, the position the
			// guard waits for will never arrive; let the next poll through.
			m.nowPlaying.resumeUntilMs = 0
		case opPlayback:
		}
		// Don't show transient network errors in the UI — they recover on their own.
		if errors.Is(msg.err, context.DeadlineExceeded) {
			if seek {
				return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
			}
			return m, nil
		}
		errCmd := m.nowPlaying.setError(userMessage(msg.err))
		if seek {
			return m, tea.Batch(
				errCmd,
				tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
			)
		}
		return m, errCmd
	}
	if seek {
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
	return m, m.seekTo(msg.posMs)
}

func (m Model) handleSeekFire(msg seekFireMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seekSeq {
		return m, nil // outdated, a newer seek superseded this one
	}
	if msg.trackURI != m.nowPlaying.trackURI {
		// The track changed during the debounce and the position was
		// computed for the old one. No seek reply will arrive to clear
		// seekPending, so release it here or polls would keep showing the
		// stale position.
		m.nowPlaying.seekPending = false
		return m, nil
	}
	return m, m.seekTo(msg.posMs)
}

// seekTo seeks the active device to posMs.
func (m Model) seekTo(posMs int) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	}, opSeek)
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
	// The list isn't on screen, so neither wheel nor click may reach it.
	// Its zones from the last list frame are still registered and would
	// otherwise resolve clicks to rows the user can't see.
	if m.listHidden() {
		return true, m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if time.Since(m.lastWheelTime) < wheelDebounceWindow {
			return true, m, nil
		}
		if s, ok := m.currentView().(scrollable); ok {
			s.scrollUp()
			m.lastWheelTime = time.Now()
			return true, m, nil
		}
	case tea.MouseButtonWheelDown:
		if time.Since(m.lastWheelTime) < wheelDebounceWindow {
			return true, m, nil
		}
		if s, ok := m.currentView().(scrollable); ok {
			s.scrollDown()
			m.lastWheelTime = time.Now()
			return true, m, m.loadNearEnd()
		}
	case tea.MouseButtonLeft:
		return m.handleMouseClick(msg)
	}
	return false, m, nil
}

// loadNearEnd asks the current view for its next page after the shell
// moved the cursor itself; see nearEndLoader.
func (m Model) loadNearEnd() tea.Cmd {
	if l, ok := m.currentView().(nearEndLoader); ok {
		return l.loadNearEnd()
	}
	return nil
}

// listHidden reports whether an overlay or mode has replaced the current
// view's list on screen.
func (m Model) listHidden() bool {
	return m.showHelp || m.showDeviceSelector || m.miniMode || m.visualizer.active
}

// handleMouseClick resolves a left-press via the current view's clickable
// implementation. Double-click within doubleClickWindow fires Enter.
func (m Model) handleMouseClick(msg tea.MouseMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	c, ok := m.currentView().(clickable)
	if !ok {
		return false, m, nil
	}
	id := c.clickAt(msg)
	if id == "" {
		return false, m, nil
	}
	return m.registerClick(id)
}

// registerClick records a click for double-click detection and fires
// handleEnter if this click closes a double-click pair on the same id.
func (m Model) registerClick(id string) (handled bool, model tea.Model, cmd tea.Cmd) {
	now := time.Now()
	if m.lastClickID == id && now.Sub(m.lastClickTime) < doubleClickWindow {
		m.lastClickID = ""
		m.lastClickTime = time.Time{}
		nm, c := m.handleEnter()
		return true, nm, c
	}
	m.lastClickID = id
	m.lastClickTime = now
	return true, m, nil
}
