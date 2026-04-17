package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// Key dispatch lives here so app.go can stay focused on the Model and the
// top-level Update switch. Every handler returns (Model, cmd, handled) —
// the bool lets handleKeyMsg fall through to the next tier without the
// handlers having to know about each other.

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m, cmd, handled := m.handleOverlayKey(msg); handled {
		return m, cmd
	}
	if m, cmd, handled := m.handleSearchInput(msg); handled {
		return m, cmd
	}
	if m.vimMode {
		if m, cmd, handled := m.handleVimKey(msg); handled {
			return m, cmd
		}
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	if m, cmd, handled := m.handlePlaybackKey(msg); handled {
		return m, cmd
	}
	if m, cmd, handled := m.handleNavigationKey(msg); handled {
		return m, cmd
	}
	return m.handleStateUpdate(msg)
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.showHelp {
		switch msg.String() {
		case "h", "?", "esc":
			m.showHelp = false
		case "ctrl+c", "q":
			return m, tea.Quit, true
		}
		return m, nil, true
	}
	if m.showDeviceSelector {
		switch msg.String() {
		case "esc", "tab":
			m.showDeviceSelector = false
		case "up", "k":
			m.deviceSelector.up()
		case "down", "j":
			m.deviceSelector.down()
		case "enter":
			if dev, ok := m.deviceSelector.selected(); ok {
				m.showDeviceSelector = false
				m.deviceSelector.transferring = true
				m.deviceSelector.transferTarget = dev.Name
				m.deviceSelector.transferDeadline = time.Now().Add(15 * time.Second)
				m.nowPlaying.recordUserAction()
				return m, m.transferDevice(dev), true
			}
		case "ctrl+c", "q":
			return m, tea.Quit, true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleSearchInput(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	// API search with debounce.
	if sv, ok := m.currentView().(*searchView); ok && sv.searching {
		sc := searchCtx{
			query: &sv.searchQuery,
			list:  &sv.list,
			close: func() { sv.closeSearch() },
			play: func(item list.Item) tea.Cmd {
				if !sv.isPlayable() {
					return sv.drillDown(item)
				}
				return sv.playSelected(&m, item)
			},
			onChange: func() tea.Cmd {
				sv.debounceSeq++
				_, term := parseSearch(sv.searchQuery)
				if len([]rune(term)) >= 2 {
					return sv.debounce()
				}
				return nil
			},
		}
		if cmd, handled := handleSearchKey(sc, msg); handled {
			return m, cmd, true
		}
		// Unhandled keys (up/down) fall through to state update.
		updated, cmd := m.handleStateUpdate(msg)
		return updated.(Model), cmd, true
	}

	// Local filter search.
	sl := m.searchableList()
	if sl != nil && sl.searching {
		sc := searchCtx{
			query: &sl.searchQuery,
			list:  &sl.list,
			close: func() { sl.closeSearch() },
			play: func(item list.Item) tea.Cmd {
				if e, ok := m.currentView().(enterable); ok {
					return e.OnEnter(&m)
				}
				return nil
			},
			onChange: func() tea.Cmd {
				sl.applyFilter()
				return nil
			},
		}
		if cmd, handled := handleSearchKey(sc, msg); handled {
			return m, cmd, true
		}
		updated, cmd := m.handleStateUpdate(msg)
		return updated.(Model), cmd, true
	}

	return m, nil, false
}

func (m Model) handleVimKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "h":
		m, cmd := m.handleBack()
		return m.(Model), cmd, true
	case "l":
		m, cmd := m.handleEnter()
		return m.(Model), cmd, true
	case ",":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(-5000), true
	case ".":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(5000), true
	case "ctrl+d":
		m, cmd := m.halfPage(1)
		return m.(Model), cmd, true
	case "ctrl+u":
		m, cmd := m.halfPage(-1)
		return m.(Model), cmd, true
	}
	return m, nil, false
}

func (m Model) handlePlaybackKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case " ":
		m.nowPlaying.recordUserAction()
		wasPlaying := m.nowPlaying.playing
		m.nowPlaying.playing = !wasPlaying
		m.nowPlaying.playPausePending = true
		return m, m.togglePlayPause(wasPlaying), true
	case "n":
		m.nowPlaying.recordUserAction()
		return m, m.nextTrack(), true
	case "p":
		m.nowPlaying.recordUserAction()
		return m, m.previousTrack(), true
	case "r":
		m.nowPlaying.recordUserAction()
		newShuffle := !m.nowPlaying.shuffling
		m.nowPlaying.shuffling = newShuffle
		m.nowPlaying.shufflePending = true
		return m, m.toggleShuffle(newShuffle), true
	case "s":
		m.nowPlaying.recordUserAction()
		return m, m.stopPlayback(), true
	case "a":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(-5000), true
	case "d":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(5000), true
	case "c":
		return m, m.copyTrackLink(), true
	}
	return m, nil, false
}

func (m Model) handleNavigationKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "v":
		if m.miniMode {
			return m, nil, true
		}
		if m.nowPlaying.hasTrack && isPlayableURI(m.nowPlaying.trackURI) {
			cmd := m.visualizer.toggle(idFromURI(m.nowPlaying.trackURI), m.nowPlaying.durationMs, m.nowPlaying.imageURL, m.nowPlaying.track, m.nowPlaying.artist, isEpisodeURI(m.nowPlaying.trackURI))
			return m, cmd, true
		}
		return m, nil, true
	case "left":
		if m.visualizer.active {
			m.visualizer.cycle(-1)
			return m, nil, true
		}
	case "right":
		if m.visualizer.active {
			m.visualizer.cycle(1)
			return m, nil, true
		}
	case "tab":
		if m.deviceSelector.transferring {
			return m, nil, true
		}
		m.deviceSelector.open()
		m.showDeviceSelector = true
		return m, fetchDevicesCmd(m.client), true
	case "m":
		m.miniMode = !m.miniMode
		return m, nil, true
	case "h", "?":
		if !m.miniMode {
			m.showHelp = true
		}
		return m, nil, true
	case "esc":
		if m.miniMode {
			m.miniMode = false
			return m, nil, true
		}
		m, cmd := m.handleBack()
		return m.(Model), cmd, true
	case "enter":
		if m.miniMode {
			return m, nil, true
		}
		m, cmd := m.handleEnter()
		return m.(Model), cmd, true
	case "/":
		if sv, ok := m.currentView().(*searchView); ok {
			sv.openSearch()
			return m, nil, true
		}
		if sl := m.searchableList(); sl != nil {
			if sl.openSearch() {
				return m, m.fetchSearchableView(), true
			}
			return m, nil, true
		}
	}
	return m, nil, false
}

// handleSearchKey dispatches a key event for a search input session.
// Returns the command and whether the key was handled (false = fall through for up/down).
func handleSearchKey(sc searchCtx, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit, true
	case "esc":
		sc.close()
		return nil, true
	case "enter":
		selected := sc.list.SelectedItem()
		// Don't try to play/drill into status rows ("Loading more…",
		// "No matching results"). Real items — tracks, episodes, albums,
		// artists, shows, playlists — all pass this check.
		if selected == nil {
			return nil, true
		}
		if _, ok := selected.(statusItem); ok {
			return nil, true
		}
		sc.close()
		return sc.play(selected), true
	case "backspace":
		runes := []rune(*sc.query)
		if len(runes) > 0 {
			*sc.query = string(runes[:len(runes)-1])
			return sc.onChange(), true
		}
		return nil, true
	case "/":
		return nil, true
	case "up", "down", "left", "right":
		return nil, false
	default:
		if len(msg.Runes) > 0 {
			*sc.query += string(msg.Runes)
			return sc.onChange(), true
		}
		return nil, true
	}
}
