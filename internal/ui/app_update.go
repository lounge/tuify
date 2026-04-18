package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) waitForLibrespotInactive() tea.Cmd {
	ch := m.librespotInactiveCh
	return func() tea.Msg {
		<-ch
		return LibrespotInactiveMsg{}
	}
}

func (m Model) waitForTokenSaveErr() tea.Cmd {
	ch := m.tokenSaveErrCh
	return func() tea.Msg {
		err, ok := <-ch
		if !ok {
			return nil
		}
		return TokenSaveErrMsg{Err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case playbackResultMsg:
		return m.handlePlaybackResult(msg)
	case vizTickMsg:
		return m.handleVizTick()
	case episodeResumeMsg:
		return m.handleEpisodeResume(msg)
	case clipboardResultMsg:
		if msg.err != nil {
			return m, m.nowPlaying.SetError("Failed to copy: " + msg.err.Error())
		}
		return m, m.nowPlaying.SetInfo("Copied link to clipboard")
	case seekFireMsg:
		return m.handleSeekFire(msg)
	case LibrespotInactiveMsg:
		m.nowPlaying.setDeviceOverride(true, "librespot inactive — playback moved away from "+m.client.PreferredDevice)
		m.nowPlaying.deviceName = ""
		return m, tea.Batch(m.nowPlaying.pollState(), m.waitForLibrespotInactive())
	case TokenSaveErrMsg:
		return m, tea.Batch(
			m.nowPlaying.SetError("Token save failed: "+msg.Err.Error()),
			m.waitForTokenSaveErr(),
		)
	case spinner.TickMsg:
		// Advance the global loading spinner and reschedule. Every frame
		// that references loadingSpinner.View() — list status rows, device
		// overlay, now-playing banner — sees the new frame on the next
		// View() call triggered by this very tick.
		var cmd tea.Cmd
		loadingSpinner, cmd = loadingSpinner.Update(msg)
		return m, cmd
	case devicesLoadedMsg:
		m.deviceSelector.handleLoaded(msg)
		return m, nil
	case transferDeviceMsg:
		if msg.err != nil {
			m.deviceSelector.transferring = false
			if errors.Is(msg.err, context.DeadlineExceeded) {
				return m, nil
			}
			return m, m.nowPlaying.SetError("Transfer failed: " + msg.err.Error())
		}
		// Update override state based on whether the chosen device is preferred.
		if m.client.PreferredDevice != "" && msg.deviceName != m.client.PreferredDevice {
			m.nowPlaying.setDeviceOverride(true, "transferred to non-preferred device "+msg.deviceName)
		} else {
			m.nowPlaying.setDeviceOverride(false, "transferred to preferred device "+msg.deviceName)
		}
		m.nowPlaying.deviceName = msg.deviceName
		return m, m.nowPlaying.SetSpinningInfo("Switching to " + msg.deviceName)
	}

	return m.handleStateUpdate(msg)
}

// handleStateUpdate processes now-playing, visualizer, and view updates.
// Called for messages not fully consumed by other handlers.
func (m Model) handleStateUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Update now-playing
	prevURI := m.nowPlaying.trackURI
	cmd := m.nowPlaying.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Clear transfer lock once the poller confirms the target device is active,
	// or if the deadline has passed.
	if m.deviceSelector.transferring {
		if m.nowPlaying.deviceName == m.deviceSelector.transferTarget ||
			time.Now().After(m.deviceSelector.transferDeadline) {
			m.deviceSelector.transferring = false
		}
	}

	// Re-init visualizer on track change and reload album art + lyrics
	if m.nowPlaying.trackURI != prevURI && isPlayableURI(m.nowPlaying.trackURI) {
		m.visualizer.onTrackChange(idFromURI(m.nowPlaying.trackURI), m.nowPlaying.durationMs, m.nowPlaying.track, m.nowPlaying.artist, isEpisodeURI(m.nowPlaying.trackURI))
		m.visualizer.loadImage(m.nowPlaying.imageURL)
		cmds = append(cmds, tea.SetWindowTitle(fmt.Sprintf("tuify — %s — %s", m.nowPlaying.track, m.nowPlaying.artist)))
	} else if m.nowPlaying.imageURL != m.visualizer.imageURL {
		m.visualizer.loadImage(m.nowPlaying.imageURL)
	}

	// Sync list selection when the playing item changes
	if m.nowPlaying.trackURI != prevURI {
		if sv, ok := m.currentView().(syncableView); ok {
			if cmd := sv.SyncURI(m.nowPlaying.trackURI); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// Update current view
	if cmd := m.currentView().Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// Navigation actions

func (m Model) handleBack() (tea.Model, tea.Cmd) {
	if m.visualizer.active {
		m.visualizer.active = false
		return m, nil
	}
	if sv, ok := m.currentView().(*searchView); ok && sv.depth > 0 {
		if sv.goBack() {
			return m, sv.goBackFetchCmd()
		}
	}
	m.popView()
	return m, nil
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if e, ok := m.currentView().(enterable); ok {
		return m, e.OnEnter(&m)
	}
	return m, nil
}

func (m Model) halfPage(dir int) (tea.Model, tea.Cmd) {
	l := m.currentList()
	if l == nil {
		return m, nil
	}
	half := m.listHeight() / 4 // list items are ~2 lines tall
	if half < 1 {
		half = 1
	}
	idx := l.Index() + dir*half
	if idx < 0 {
		idx = 0
	}
	if max := len(l.Items()) - 1; idx > max {
		idx = max
	}
	l.Select(idx)
	return m, nil
}
