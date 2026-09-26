package ui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// The loading spinner and the now-playing label marquee are self-
// rescheduling tick chains. Each chain runs only while something on
// screen uses it: its tick handler stops rescheduling once idle, and
// Update calls resumeTickers after every message to restart a chain the
// new state needs. The *Ticking flags make sure at most one tick of each
// kind is ever in flight.

// resumeTickers starts any idle tick chain the current state needs.
func (m *Model) resumeTickers() tea.Cmd {
	var cmds []tea.Cmd
	if !m.spinnerTicking && m.needsSpinner() {
		m.spinnerTicking = true
		cmds = append(cmds, loadingSpinner.Tick)
	}
	if !m.nowPlaying.labelTicking && m.labelNeedsScroll() {
		m.nowPlaying.labelTicking = true
		cmds = append(cmds, m.nowPlaying.labelScrollTick())
	}
	return tea.Batch(cmds...)
}

// handleSpinnerTick advances the global loading spinner and reschedules
// while anything on screen spins. Every frame that references
// loadingSpinner.View() — list status rows, device overlay, now-playing
// banner — sees the new frame on the View() call after this tick.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.needsSpinner() {
		m.spinnerTicking = false
		return m, nil
	}
	var cmd tea.Cmd
	loadingSpinner, cmd = loadingSpinner.Update(msg)
	return m, cmd
}

// handleLabelScroll advances the marquee and reschedules while the label
// overflows. Once it fits, the chain stops and the offset resets so the
// next overflow scrolls from the start.
func (m Model) handleLabelScroll() (tea.Model, tea.Cmd) {
	if !m.labelNeedsScroll() {
		m.nowPlaying.labelTicking = false
		m.nowPlaying.labelScrollOffset = 0
		return m, nil
	}
	m.nowPlaying.advanceLabelScroll()
	return m, m.nowPlaying.labelScrollTick()
}

// needsSpinner reports whether anything currently rendered shows the
// loading spinner: the now-playing status banner, the device overlay
// while it loads, or a spinning status row in the current list. Spinning
// rows are only ever the sole row or the trailing "Loading more…" row.
func (m Model) needsSpinner() bool {
	if m.nowPlaying.statusMsg != "" && m.nowPlaying.statusSpinning {
		return true
	}
	if m.showDeviceSelector && m.deviceSelector.loading {
		return true
	}
	if l := m.currentList(); l != nil {
		if items := l.Items(); len(items) > 0 {
			return isSpinningRow(items[0]) || isSpinningRow(items[len(items)-1])
		}
	}
	return false
}

func isSpinningRow(item list.Item) bool {
	si, ok := item.(statusItem)
	return ok && si.spinning && !si.isError
}

// labelNeedsScroll reports whether the "track — artist" label overflows
// the budget of the layout currently shown, i.e. whether the marquee
// moves.
func (m Model) labelNeedsScroll() bool {
	np := m.nowPlaying
	if !np.hasTrack {
		return false
	}
	if m.miniMode {
		return np.labelOverflows(m.miniLabelBudget())
	}
	budget, _ := np.trackLineLabelBudget()
	return np.labelOverflows(budget)
}
