package ui

import (
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
	client      *spotify.Client
	searching   bool
	searchQuery string // raw user input (e.g. "a:queen")
	query       string // committed search term (after prefix, e.g. "queen")
	prefix      searchPrefix
	debounceSeq int
	epoch       int // incremented on every state reset to discard stale results
	depth       int // 0 = search results, 1 = container detail, 2 = artist→album→tracks
	offset      int
	hasMore     bool
	pending     int
	searchErr   error
	syncURI     string

	// drill-down state
	selectedArtist selectedRef
	selectedAlbum  selectedRef
	selectedShow   selectedRef

	// items backing the list at current depth
	items []list.Item
}

func newSearchView(client *spotify.Client, width, height int, vimMode bool) *searchView {
	l := newList(width, height, vimMode)
	l.SetItems(nil)
	return &searchView{
		list:      l,
		client:    client,
		searching: true,
	}
}

// Lifecycle helpers

func (v *searchView) closeSearch() {
	v.searching = false
	v.searchQuery = ""
}

func (v *searchView) openSearch() {
	v.searching = true
	v.searchQuery = ""
	v.resetToDepth0()
}

func (v *searchView) resetToDepth0() {
	v.depth = 0
	v.items = nil
	v.offset = 0
	v.hasMore = false
	v.pending = 0
	v.query = ""
	v.searchErr = nil
	v.syncURI = ""
	v.selectedArtist = selectedRef{}
	v.selectedAlbum = selectedRef{}
	v.selectedShow = selectedRef{}
	v.list.SetItems(nil)
}

// resetPagination clears pagination state for a new depth level.
func (v *searchView) resetPagination() {
	v.epoch++
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
		v.epoch++
		v.items = nil
		v.offset = 0
		v.hasMore = false
		v.pending = 1
		v.searchErr = nil
		v.selectedArtist = selectedRef{}
		v.selectedAlbum = selectedRef{}
		v.selectedShow = selectedRef{}
		v.list.SetItems([]list.Item{loadingStatusItem})
		return v.fetchResults(term, 0, 10)

	case searchResultMsg:
		if msg.epoch != v.epoch {
			return nil
		}
		v.pending--
		if msg.err != nil {
			v.searchErr = msg.err
			v.hasMore = false
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
	cmds := []tea.Cmd{cmd}

	// Pagination: fetch more when near bottom
	if v.pending == 0 && len(v.items) > 0 && len(v.items)-v.list.Index() <= 10 {
		if cmd := v.fetchMore(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// View-interface methods

func (v *searchView) SetSize(width, height int) {
	v.list.SetSize(width, height)
}

func (v *searchView) List() *list.Model {
	return &v.list
}

func (v *searchView) SyncURI(uri string) tea.Cmd {
	if !v.isPlayable() {
		return nil
	}
	if v.selectByURI(uri) {
		return v.fetchMore()
	}
	return nil
}

func (v searchView) View() string {
	if v.depth == 0 && v.query == "" && len(v.items) == 0 && v.pending == 0 {
		box := searchHintBoxStyle.Render(searchHintText)
		return lipgloss.Place(v.list.Width(), v.list.Height(), lipgloss.Center, lipgloss.Center, box)
	}
	return v.list.View()
}
