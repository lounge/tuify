package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
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

var loadingStyle = lipgloss.NewStyle().Foreground(colorSubtle)

func (i statusItem) Title() string {
	if i.isError {
		return errorStyle.Render(i.text)
	}
	return loadingStyle.Render(i.text)
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

// lazyList holds the shared state and logic for paginated list views.
type lazyList struct {
	list    list.Model
	items   []list.Item
	offset  int
	loading bool
	hasMore bool
}

func newLazyList(width, height int) lazyList {
	l := newList(width, height)
	initial := []list.Item{statusItem{text: "Loading..."}}
	l.SetItems(initial)
	return lazyList{
		list:    l,
		items:   initial,
		loading: true,
		hasMore: true,
	}
}

// onLoaded clears the loading indicator. Call at the start of a loaded-msg handler.
func (l *lazyList) onLoaded() {
	l.loading = false
	l.items = removeStatusItems(l.items)
}

// onError sets the error state with a retry prompt.
func (l *lazyList) onError(err error) {
	l.hasMore = false
	l.items = append(l.items, statusItem{
		text:    fmt.Sprintf("Failed to load: %v — press Enter to retry", err),
		isError: true,
	})
	l.list.SetItems(l.items)
}

// append adds items, advances the offset, and refreshes the list widget.
func (l *lazyList) append(items []list.Item, fetched int, hasMore bool) {
	l.items = append(l.items, items...)
	l.offset += fetched
	l.hasMore = hasMore
	l.list.SetItems(l.items)
}

// triggerLoad checks whether the cursor is near the end of the loaded items
// and, if so, starts a loading state. Returns true when loading was triggered
// and the caller should issue a fetch command.
func (l *lazyList) triggerLoad() bool {
	if l.loading || !l.hasMore {
		return false
	}
	if len(l.items)-l.list.Index() <= 10 {
		l.loading = true
		l.items = append(l.items, statusItem{text: "Loading..."})
		l.list.SetItems(l.items)
		return true
	}
	return false
}

// retryLoad resets the list into a loading state for a retry.
func (l *lazyList) retryLoad() {
	l.hasMore = true
	l.loading = true
	l.items = removeStatusItems(l.items)
	l.items = append(l.items, statusItem{text: "Loading..."})
	l.list.SetItems(l.items)
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
