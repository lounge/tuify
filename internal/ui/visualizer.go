package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type vizTickMsg struct{}

type visualizerModel struct {
	active  bool
	trackID string
	vizList []visualizers.Visualizer
	vizIdx  int
}

func newVisualizerModel() visualizerModel {
	return visualizerModel{
		vizList: []visualizers.Visualizer{
			visualizers.NewOscillogram(),
			visualizers.NewStarfield(),
		},
	}
}

func (m *visualizerModel) viz() visualizers.Visualizer {
	return m.vizList[m.vizIdx]
}

func (m *visualizerModel) toggle(trackID string, durationMs int) tea.Cmd {
	if m.active {
		m.active = false
		return nil
	}
	m.active = true
	if trackID != m.trackID {
		m.trackID = trackID
		for _, v := range m.vizList {
			v.Init(trackID, durationMs)
		}
	}
	return m.tick()
}

func (m visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance() {
	for _, v := range m.vizList {
		v.Advance()
	}
}

func (m *visualizerModel) cycle(delta int) {
	m.vizIdx = (m.vizIdx + delta + len(m.vizList)) % len(m.vizList)
}

func (m *visualizerModel) onTrackChange(trackID string, durationMs int) {
	if !m.active {
		return
	}
	m.trackID = trackID
	for _, v := range m.vizList {
		v.Init(trackID, durationMs)
	}
}

func (m visualizerModel) View(progressMs, width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	vizHeight := height
	if height > 2 {
		vizHeight = height - 1
		viz := m.viz().View(progressMs, width, vizHeight)
		hint := helpStyle.Render("← →")
		hintLine := lipgloss.PlaceHorizontal(width, lipgloss.Center, hint)
		return viz + "\n" + hintLine
	}
	return m.viz().View(progressMs, width, vizHeight)
}

func isTrackURI(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:")
}

func trackIDFromURI(uri string) string {
	return strings.TrimPrefix(uri, "spotify:track:")
}
