package ui

import (
	"fmt"
	"strings"
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
	list        list.Model
	items       []list.Item
	offset      int
	loading     bool
	hasMore     bool
	searching   bool
	searchQuery string
	syncURI     string
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
// During search, it re-applies the filter and returns true if more data should
// be fetched to complete the search across all items.
func (l *lazyList) append(items []list.Item, fetched int, hasMore bool) bool {
	l.items = append(l.items, items...)
	l.offset += fetched
	l.hasMore = hasMore
	if l.searching {
		l.applyFilter()
		if l.hasMore {
			l.loading = true
			return true
		}
		return false
	}
	l.list.SetItems(l.items)
	return false
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

// openSearch enters search mode. Returns true if the caller should trigger
// a fetch to load remaining items.
func (l *lazyList) openSearch() bool {
	l.searching = true
	l.searchQuery = ""
	if l.hasMore && !l.loading {
		l.loading = true
		return true
	}
	return false
}

func (l *lazyList) closeSearch() {
	l.searching = false
	l.searchQuery = ""
	l.list.SetItems(l.items)
}

// findByURI locates an item by URI. Items must implement URI() string.
func (l *lazyList) findByURI(uri string) (int, bool) {
	for i, item := range l.items {
		if u, ok := item.(interface{ URI() string }); ok && u.URI() == uri {
			return i, true
		}
	}
	return 0, false
}

// selectByURI selects the item matching uri, or sets syncURI for deferred
// resolution. Returns true if the caller should fetch more data.
func (l *lazyList) selectByURI(uri string) bool {
	if i, ok := l.findByURI(uri); ok {
		l.list.Select(i)
		l.syncURI = ""
		return false
	}
	l.syncURI = uri
	if l.hasMore && !l.loading {
		l.loading = true
		return true
	}
	return false
}

// resolveSync tries to select the pending syncURI after new items are loaded.
// Returns true if the caller should fetch more data.
func (l *lazyList) resolveSync() bool {
	if l.syncURI == "" {
		return false
	}
	if i, ok := l.findByURI(l.syncURI); ok {
		l.list.Select(i)
		l.syncURI = ""
		return false
	}
	if l.hasMore {
		l.loading = true
		return true
	}
	l.syncURI = ""
	return false
}

func (l *lazyList) applyFilter() {
	if l.searchQuery == "" {
		l.list.SetItems(l.items)
		return
	}
	query := strings.ToLower(l.searchQuery)
	var filtered []list.Item
	for _, item := range l.items {
		if _, ok := item.(statusItem); ok {
			continue
		}
		di, ok := item.(list.DefaultItem)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(di.Title()), query) ||
			strings.Contains(strings.ToLower(di.Description()), query) {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		l.list.SetItems([]list.Item{statusItem{text: "No matching results"}})
	} else {
		l.list.SetItems(filtered)
	}
}

// Playback result message — used for all device-bound commands.
type playbackResultMsg struct {
	deviceID string
	err      error
	seek     bool // true for seek results (uses lighter post-action polling)
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
