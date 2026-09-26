package ui

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// listFetchTimeout bounds one list or search fetch Cmd. The auth HTTP client
// has dial and response-header timeouts but no bound on reading the body, so
// without this a stalled response would leave a list stuck in its loading
// state until the app exits. For the playlist page loop it covers the whole
// loop, not each page.
const listFetchTimeout = 30 * time.Second

// pageLoader fetches one page starting at offset and maps it to list items.
// fetched is how far the offset advances, which can differ from len(items)
// when the loader skips entries. It runs inside a tea.Cmd, so it must only
// capture values that are safe to use off the Update goroutine.
type pageLoader func(ctx context.Context, offset int) (items []list.Item, fetched int, hasMore bool, err error)

// lazyList holds the shared state and logic for paginated list views: it
// owns the fetch loop, the loading/error rows, retries, local filtering and
// deferred selection. Screens embed it and add only item mapping (via their
// pageLoader), Enter handling and a breadcrumb.
type lazyList struct {
	list        list.Model
	items       []list.Item
	offset      int
	loading     bool
	hasMore     bool
	searching   bool
	searchQuery string
	syncURI     string

	// id tags every page this list requests. The shell routes a
	// pageLoadedMsg only to the list with the same id, so a late page from
	// a popped screen can't land in a newer screen of the same type.
	id   uint64
	ctx  context.Context
	load pageLoader
}

// pageLoadedMsg carries one fetched page back to the lazyList that asked
// for it.
type pageLoadedMsg struct {
	listID  uint64
	items   []list.Item
	fetched int
	hasMore bool
	err     error
}

// fetchIDs issues ids that are unique across every list and search view for
// the life of the process, so results can always be matched to their owner.
var fetchIDs atomic.Uint64

func newFetchID() uint64 { return fetchIDs.Add(1) }

func newLazyList(ctx context.Context, load pageLoader, width, height int, vimMode bool) lazyList {
	l := newList(width, height, vimMode)
	initial := []list.Item{loadingStatusItem}
	l.SetItems(initial)
	return lazyList{
		list:    l,
		items:   initial,
		loading: true,
		hasMore: true,
		id:      newFetchID(),
		ctx:     ctx,
		load:    load,
	}
}

// Init starts loading the first page. The shell calls it when it pushes the
// screen.
func (l *lazyList) Init() tea.Cmd {
	return l.fetchMore()
}

// fetchMore returns a Cmd that loads the page at the current offset.
func (l *lazyList) fetchMore() tea.Cmd {
	id, offset, parent, load := l.id, l.offset, l.ctx, l.load
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, listFetchTimeout)
		defer cancel()
		items, fetched, hasMore, err := load(ctx, offset)
		return pageLoadedMsg{listID: id, items: items, fetched: fetched, hasMore: hasMore, err: err}
	}
}

// onPage applies a loaded page and returns a Cmd for the next page when the
// active filter or a pending selection needs more data. Pages for another
// list are ignored.
func (l *lazyList) onPage(msg pageLoadedMsg) tea.Cmd {
	if msg.listID != l.id {
		return nil
	}
	l.onLoaded()
	if msg.err != nil {
		l.onError(msg.err)
		return nil
	}
	// While a search filter is active, append asks for the next page so
	// the filter covers every item, not just those loaded so far.
	if l.append(msg.items, msg.fetched, msg.hasMore) || l.resolveSync() {
		return l.fetchMore()
	}
	return nil
}

// Update applies loaded pages and forwards everything else to the inner
// list, fetching the next page when the cursor nears the end.
func (l *lazyList) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(pageLoadedMsg); ok {
		return l.onPage(msg)
	}
	return l.updateList(msg)
}

// retryOnError reloads after a failed fetch when the selected row is the
// error row. handled is false when some other row is selected.
func (l *lazyList) retryOnError() (cmd tea.Cmd, handled bool) {
	if si, ok := l.list.SelectedItem().(statusItem); ok && si.isError {
		l.prepareRetry()
		return l.fetchMore(), true
	}
	return nil, false
}

// searchableList exposes the list for local filtering. Satisfies
// searchableListProvider.
func (l *lazyList) searchableList() *lazyList { return l }

// onLoaded clears the loading indicator. Call at the start of a loaded-msg handler.
func (l *lazyList) onLoaded() {
	l.loading = false
	l.items = removeStatusItems(l.items)
}

// onError sets the error state with a retry prompt.
func (l *lazyList) onError(err error) {
	log.Printf("[list] load failed: %v", err)
	l.hasMore = false
	l.items = append(l.items, statusItem{
		text:    "Failed to load: " + userMessage(err),
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
// the cursor is near the end.
func (l *lazyList) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.list, cmd = l.list.Update(msg)
	cmds := []tea.Cmd{cmd}
	if l.triggerLoad() {
		cmds = append(cmds, l.fetchMore())
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
func (l *lazyList) listModel() *list.Model {
	return &l.list
}

// scrollUp moves the cursor one item up. Satisfies the scrollable
// interface so the mouse-wheel dispatch in handleMouse doesn't need
// to type-assert against concrete view types.
func (l *lazyList) scrollUp() { l.list.CursorUp() }

// scrollDown moves the cursor one item down.
func (l *lazyList) scrollDown() { l.list.CursorDown() }

// clickAt resolves a left-click against the zone-marked items on the
// visible list. Selects the matched item and returns its URI; empty
// return means the click missed every zoned row. Satisfies clickable.
func (l *lazyList) clickAt(msg tea.MouseMsg) string {
	for i, item := range l.list.Items() {
		u, ok := item.(uriItem)
		if !ok || u.URI() == "" {
			continue
		}
		if zone.Get(u.URI()).InBounds(msg) {
			l.list.Select(i)
			return u.URI()
		}
	}
	return ""
}

// SearchState reports whether the view is in filter-search mode.
// Satisfies searchAware.
func (l *lazyList) searchState() (bool, string) { return l.searching, l.searchQuery }

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
//
// Important: findByURI returns an index into l.items (the full backing
// slice), but l.list.Select operates on the bubbles list's currently-
// visible items. During filter mode (l.searching == true) those two
// slices diverge — selecting by an l.items index would set Paginator.Page
// past the visible-list bounds and panic on the next render. So while
// filtering we just queue the URI for later resolution.
func (l *lazyList) selectByURI(uri string) bool {
	if l.searching {
		l.syncURI = uri
		return false
	}
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
// Returns true if the caller should fetch more data. Skipped while
// filtering for the same reason as selectByURI: the bubbles list shows a
// subset of l.items and a stale index would panic on render.
func (l *lazyList) resolveSync() bool {
	if l.syncURI == "" || l.searching {
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
			displayed = []list.Item{statusItem{text: "Searching…", desc: "loading more tracks", spinning: true}}
		case len(filtered) == 0:
			displayed = []list.Item{statusItem{text: "No matching results"}}
		case pending:
			// Build a fresh slice rather than append into filtered's
			// backing array — appending to a slice you didn't own is the
			// classic source of aliasing bugs even when it "works" today.
			displayed = make([]list.Item, 0, len(filtered)+1)
			displayed = append(displayed, filtered...)
			displayed = append(displayed, statusItem{text: "Loading more…", spinning: true})
		default:
			displayed = filtered
		}
	}
	l.setItemsResetCursor(displayed)
}

// setItemsResetCursor replaces items, preserving the selected item by URI
// when possible. Bubbles' list.Model restores the old position via
// Page*PerPage+cursor after SetItems, which can leave the restored index
// past the end of the new items slice and panic on the next render.
// Calling ResetSelected() before SetItems zeroes both Paginator.Page and
// the cursor (it's defined as Select(0)), so the restored index is always
// in-bounds; we then re-select the previous item by URI if it's still
// visible.
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
