package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type captureStartedMsg struct{ err error }

type visualizerModel struct {
	active  bool
	errMsg  string
	capture *audio.Capture
	viz     visualizers.Visualizer
}

func newVisualizerModel() visualizerModel {
	return visualizerModel{
		capture: audio.NewCapture(),
		viz:     visualizers.NewOscillogram(),
	}
}

func (m *visualizerModel) toggle() tea.Cmd {
	if m.active {
		m.active = false
		m.errMsg = ""
		m.capture.Stop()
		return nil
	}
	m.active = true
	m.errMsg = ""
	capture := m.capture
	return func() tea.Msg {
		err := capture.Start()
		return captureStartedMsg{err: err}
	}
}

func (m *visualizerModel) handleCaptureStarted(msg captureStartedMsg) {
	if msg.err != nil {
		m.errMsg = "Audio capture unavailable: " + msg.err.Error()
	}
}

func (m *visualizerModel) close() {
	if m.active {
		m.active = false
		m.capture.Stop()
	}
}

func (m visualizerModel) View(width, height int) string {
	if m.errMsg != "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render(m.errMsg))
	}
	if !m.capture.Running() {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("Starting capture..."))
	}
	return m.viz.View(m.capture, width, height)
}
