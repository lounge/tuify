package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

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

func newLazyList(width, height int, vimMode bool) lazyList {
	l := newList(width, height, vimMode)
	initial := []list.Item{loadingStatusItem}
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
		text:    fmt.Sprintf("Failed to load: %v", err),
		desc:    "press Enter to retry",
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
		l.items = append(l.items, loadingStatusItem)
		l.list.SetItems(l.items)
		return true
	}
	return false
}

// prepareRetry resets the list into a loading state for a retry.
func (l *lazyList) prepareRetry() {
	l.hasMore = true
	l.loading = true
	l.items = removeStatusItems(l.items)
	l.items = append(l.items, loadingStatusItem)
	l.list.SetItems(l.items)
}

// updateList forwards a message to the inner list and triggers a fetch if
// the cursor is near the end. Use as the default branch in view Update methods.
func (l *lazyList) updateList(msg tea.Msg, fetchMore func() tea.Cmd) tea.Cmd {
	var cmd tea.Cmd
	l.list, cmd = l.list.Update(msg)
	cmds := []tea.Cmd{cmd}
	if l.triggerLoad() {
		cmds = append(cmds, fetchMore())
	}
	return tea.Batch(cmds...)
}

// View renders the inner list.
func (l lazyList) View() string {
	return l.list.View()
}

// SetSize resizes the inner list.
func (l *lazyList) SetSize(width, height int) {
	l.list.SetSize(width, height)
}

// List returns a pointer to the inner list.
func (l *lazyList) List() *list.Model {
	return &l.list
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
	selected := l.list.SelectedItem()
	l.list.SetItems(l.items)
	if u, ok := selected.(uriItem); ok {
		if i, found := l.findByURI(u.URI()); found {
			l.list.Select(i)
			return
		}
	}
	if l.list.Index() >= len(l.items) {
		l.list.ResetSelected()
	}
}

// findByURI locates an item by URI. Items must implement URI() string.
func (l *lazyList) findByURI(uri string) (int, bool) {
	for i, item := range l.items {
		if u, ok := item.(uriItem); ok && u.URI() == uri {
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
	var displayed []list.Item
	if l.searchQuery == "" {
		displayed = l.items
	} else {
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
		pending := l.hasMore || l.loading
		switch {
		case len(filtered) == 0 && pending:
			displayed = []list.Item{statusItem{text: "Searching…", desc: "loading more tracks"}}
		case len(filtered) == 0:
			displayed = []list.Item{statusItem{text: "No matching results"}}
		case pending:
			displayed = append(filtered, statusItem{text: "Loading more…"})
		default:
			displayed = filtered
		}
	}
	l.setItemsResetCursor(displayed)
}

// setItemsResetCursor replaces items, preserving the selected item by URI
// when possible. Bubbles' list.Model restores the old cursor position via
// Page*PerPage+cursor after SetItems, which can leave Page*PerPage past the
// end of the new items slice and panic on the next render. Resetting the
// cursor to 0 before SetItems makes the paginator's restored index safe, and
// we then re-select the previous item by URI if it's still visible.
func (l *lazyList) setItemsResetCursor(items []list.Item) {
	var selectedURI string
	if u, ok := l.list.SelectedItem().(uriItem); ok {
		selectedURI = u.URI()
	}
	l.list.ResetSelected()
	l.list.SetItems(items)
	if selectedURI == "" {
		return
	}
	for i, item := range items {
		if u, ok := item.(uriItem); ok && u.URI() == selectedURI {
			l.list.Select(i)
			return
		}
	}
}

func removeStatusItems(items []list.Item) []list.Item {
	out := make([]list.Item, 0, len(items))
	for _, item := range items {
		if _, ok := item.(statusItem); !ok {
			out = append(out, item)
		}
	}
	return out
}
