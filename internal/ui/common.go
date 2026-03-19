package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
)

func newList(width, height int) list.Model {
	l := list.New(nil, newListDelegate(), width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	return l
}

type statusItem struct {
	text    string
	isError bool
}

func (i statusItem) Title() string {
	if i.isError {
		return errorStyle.Render(i.text)
	}
	return i.text
}
func (i statusItem) Description() string { return "" }
func (i statusItem) FilterValue() string { return "" }

func removeStatusItems(items []list.Item) []list.Item {
	filtered := items[:0]
	for _, item := range items {
		if _, ok := item.(statusItem); !ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// Playback messages
type playResultMsg struct {
	deviceID string
	err      error
}

type playbackResultMsg struct{ err error }

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
