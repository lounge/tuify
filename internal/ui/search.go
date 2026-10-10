package ui

import (
	"context"
	"log"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

const maxQueueURIs = 50

// searchView drives the search UI: a query box + results list + drill-down
// navigation (e.g. artist → albums → tracks). Item types live in
// search_items.go, prefix parsing + messages in search_parse.go, fetch and
// pagination in search_fetch.go, drill-down and playback in search_nav.go.
type searchView struct {
	list        list.Model
	ctx         context.Context
	cancel      context.CancelFunc // ends ctx when the screen is popped (close)
	client      *spotify.Client
	searching   bool
	searchQuery string // raw user input (e.g. "a:queen")
	query       string // committed search term (after prefix, e.g. "queen")
	prefix      searchPrefix
	debounceSeq int
	// id tags the rows this view renders (see zoneListDelegate). Unlike
	// epoch it never changes, so a click resolves against the rows on
	// screen whichever search produced them.
	id        uint64
	epoch     uint64 // replaced on every state reset (newFetchID) to discard stale results
	depth     int    // 0 = search results, 1 = container detail, 2 = artist→album→tracks
	offset    int
	hasMore   bool
	pending   int
	searchErr error
	syncURI   string

	// drill-down state
	selectedArtist selectedRef
	selectedAlbum  selectedRef
	selectedShow   selectedRef

	// items backing the list at current depth
	items []list.Item
}

func newSearchView(ctx context.Context, client *spotify.Client, width, height int, vimMode bool) *searchView {
	id := newFetchID()
	l := newList(id, width, height, vimMode)
	l.SetItems(nil)
	ctx, cancel := context.WithCancel(ctx)
	return &searchView{
		list:      l,
		ctx:       ctx,
		cancel:    cancel,
		client:    client,
		searching: true,
		id:        id,
		// Epochs come from the same process-wide counter as list ids, so a
		// result for an earlier search view can never match this one.
		epoch: newFetchID(),
	}
}

// Lifecycle helpers

// closeSearch ends the input session. A debounce tick scheduled by the
// last edit must not run after Enter or Esc: it would replace what Enter
// opened (a drill-down, the queue it started) with a fresh search.
func (v *searchView) closeSearch() {
	v.searching = false
	v.searchQuery = ""
	v.debounceSeq++
}

func (v *searchView) openSearch() {
	v.searching = true
	v.searchQuery = ""
	v.resetToDepth0()
}

// openSearchInput implements inputSearcher.
func (v *searchView) openSearchInput() { v.openSearch() }

// activeSearchInput implements inputSearcher: while the search input is
// open, it returns the session the shell routes key presses through.
// Enter drills into containers and plays playable items; each edit
// restarts the debounce once the term is at least two runes long.
func (v *searchView) activeSearchInput() (searchCtx, bool) {
	if !v.searching {
		return searchCtx{}, false
	}
	return searchCtx{
		query: &v.searchQuery,
		list:  &v.list,
		close: v.closeSearch,
		play: func(item list.Item) tea.Cmd {
			if !v.isPlayable() {
				return v.drillDown(item)
			}
			return v.playSelected(item)
		},
		retry: v.retry,
		onChange: func() tea.Cmd {
			v.debounceSeq++
			_, term := parseSearch(v.searchQuery)
			if len([]rune(term)) >= 2 {
				return v.debounce()
			}
			return nil
		},
	}, true
}

func (v *searchView) resetToDepth0() {
	v.resetPagination()
	v.depth = 0
	v.query = ""
	v.selectedArtist = selectedRef{}
	v.selectedAlbum = selectedRef{}
	v.selectedShow = selectedRef{}
	v.list.SetItems(nil)
}

// resetPagination clears pagination state for a new depth level. A fetch
// or debounce still in flight belongs to the session being discarded:
// the epoch rotates so a late result is dropped instead of filling the
// emptied list and driving pending negative, and debounceSeq is bumped so
// a tick scheduled before the reset does not run its stale query over
// the new level.
func (v *searchView) resetPagination() {
	v.epoch = newFetchID()
	v.debounceSeq++
	v.items = nil
	v.offset = 0
	v.hasMore = false
	v.pending = 0
	v.searchErr = nil
	v.syncURI = ""
}

// startLoading resets pagination and puts the view into loading state.
func (v *searchView) startLoading() {
	v.resetPagination()
	v.pending = 1
	v.list.SetItems([]list.Item{loadingStatusItem})
}

func (v searchView) debounce() tea.Cmd {
	seq := v.debounceSeq
	query := v.searchQuery
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return searchDebounceMsg{seq: seq, query: query}
	})
}

// Update

func (v *searchView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		if msg.seq != v.debounceSeq {
			return nil
		}
		prefix, term := parseSearch(msg.query)
		v.prefix = prefix
		v.query = term
		v.depth = 0
		v.epoch = newFetchID()
		v.items = nil
		v.offset = 0
		v.hasMore = false
		v.pending = 1
		v.searchErr = nil
		v.selectedArtist = selectedRef{}
		v.selectedAlbum = selectedRef{}
		v.selectedShow = selectedRef{}
		v.list.SetItems([]list.Item{loadingStatusItem})
		return v.fetchResults(term, 0)

	case searchResultMsg:
		if msg.epoch != v.epoch {
			return nil
		}
		v.pending--
		if msg.err != nil {
			// hasMore is kept: the failed page is still there to fetch, and
			// retry resumes at the same offset. fetchMore holds off while
			// searchErr is set, so the error row is the only way on.
			log.Printf("[search] fetch failed: %v", msg.err)
			v.searchErr = msg.err
		} else {
			v.items = append(v.items, msg.items...)
			v.offset += len(msg.items)
			v.hasMore = msg.hasMore
		}
		v.rebuildList()
		// resolve sync
		if v.syncURI != "" && v.pending == 0 && v.hasMore {
			return v.fetchMore()
		}
		return nil
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	return tea.Batch(cmd, v.loadNearEnd())
}

// loadNearEnd fetches the next page once the cursor is within
// searchFetchAhead rows of the end of the loaded results. Satisfies
// nearEndLoader; Update calls it after every key the list handled, and the
// shell after moving the cursor itself (wheel, half page).
func (v *searchView) loadNearEnd() tea.Cmd {
	if len(v.items) > 0 && len(v.items)-v.list.Index() <= searchFetchAhead {
		return v.fetchMore()
	}
	return nil
}

// View-interface methods

func (v *searchView) SetSize(width, height int) {
	v.list.SetSize(width, height)
}

func (v *searchView) listModel() *list.Model {
	return &v.list
}

// scrollUp / scrollDown / clickAt / back / searchState satisfy the
// capability interfaces in common.go so Model.Update doesn't have to
// type-assert against *searchView.

func (v *searchView) scrollUp()   { v.list.CursorUp() }
func (v *searchView) scrollDown() { v.list.CursorDown() }

func (v *searchView) clickAt(msg tea.MouseMsg) string {
	return clickRow(&v.list, v.id, msg)
}

func (v *searchView) back() (tea.Cmd, bool) {
	if v.depth == 0 {
		return nil, false
	}
	if v.goBack() {
		return v.goBackFetchCmd(), true
	}
	return nil, false
}

func (v *searchView) searchState() (bool, string) { return v.searching, v.searchQuery }

// close abandons any fetch still in flight. Satisfies closer.
func (v *searchView) close() { v.cancel() }

// syncTo implements syncableView. Only a drilled-into album or show has a
// context of its own to page through; track and episode results at depth
// 0 play as a queue built from the loaded results, so the playing item is
// found among them or not at all.
func (v *searchView) syncTo(uri, contextURI string) tea.Cmd {
	if !v.isPlayable() {
		return nil
	}
	if own := v.contextURI(); own == "" || contextURI != own {
		v.selectLoadedByURI(uri)
		return nil
	}
	if v.selectByURI(uri) {
		return v.fetchMore()
	}
	return nil
}

func (v searchView) View() string {
	if v.depth == 0 && v.query == "" && len(v.items) == 0 && v.pending == 0 {
		box := searchHintBoxStyle.Render(searchHintText())
		return lipgloss.Place(v.list.Width(), v.list.Height(), lipgloss.Center, lipgloss.Center, box)
	}
	return v.list.View()
}
