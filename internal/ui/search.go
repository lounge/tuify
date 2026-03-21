package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

const (
	tabTracks      = 0
	tabEpisodes    = 1
	searchTabLines = 1
)

type searchDebounceMsg struct {
	seq   int
	query string
}

type searchTracksMsg struct {
	tracks  []spotify.Track
	hasMore bool
	query   string
	err     error
}

type searchEpisodesMsg struct {
	episodes []spotify.Episode
	hasMore  bool
	query    string
	err      error
}

type searchView struct {
	list           list.Model
	client         *spotify.Client
	searching      bool
	searchQuery    string
	query          string // committed query (results are for this)
	debounceSeq    int
	tracks         []trackItem
	episodes       []episodeItem
	trackOffset    int
	episodeOffset  int
	trackHasMore   bool
	episodeHasMore bool
	trackPending   int
	episodePending int
	syncURI        string // deferred URI to select after more items load
	searchErr      error  // last search error, shown when no results
	tab            int    // 0 = tracks, 1 = episodes
}

func newSearchView(client *spotify.Client, width, height int) searchView {
	l := newList(width, height-searchTabLines)
	l.SetItems([]list.Item{statusItem{text: "Type to search..."}})
	return searchView{
		list:      l,
		client:    client,
		searching: true,
	}
}

func (v *searchView) closeSearch() {
	v.searching = false
	v.searchQuery = ""
}

func (v *searchView) openSearch() {
	v.searching = true
	v.searchQuery = ""
}

func (v searchView) debounce() tea.Cmd {
	seq := v.debounceSeq
	query := v.searchQuery
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return searchDebounceMsg{seq: seq, query: query}
	})
}

func (v searchView) fetchTracks(query string, offset, limit int) tea.Cmd {
	client := v.client
	return func() tea.Msg {
		tracks, hasMore, err := client.SearchTracks(context.Background(), query, offset, limit)
		return searchTracksMsg{tracks: tracks, hasMore: hasMore, query: query, err: err}
	}
}

func (v searchView) fetchEpisodes(query string, offset, limit int) tea.Cmd {
	client := v.client
	return func() tea.Msg {
		episodes, hasMore, err := client.SearchEpisodes(context.Background(), query, offset, limit)
		return searchEpisodesMsg{episodes: episodes, hasMore: hasMore, query: query, err: err}
	}
}

func (v *searchView) tabPending() int {
	if v.tab == tabTracks {
		return v.trackPending
	}
	return v.episodePending
}

// resolveSyncFetch triggers more fetches if syncURI is still pending after a rebuild.
func (v *searchView) resolveSyncFetch() []tea.Cmd {
	if v.syncURI == "" || v.tabPending() > 0 {
		return nil
	}
	return v.fetchMore()
}

func (v *searchView) fetchMore() []tea.Cmd {
	var cmds []tea.Cmd
	switch v.tab {
	case tabTracks:
		if v.trackHasMore && v.trackPending == 0 {
			v.trackPending++
			cmds = append(cmds, v.fetchTracks(v.query, v.trackOffset, 10))
		}
	case tabEpisodes:
		if v.episodeHasMore && v.episodePending == 0 {
			v.episodePending++
			cmds = append(cmds, v.fetchEpisodes(v.query, v.episodeOffset, 10))
		}
	}
	return cmds
}

func (v *searchView) rebuildList() {
	prev := v.list.Index()
	var items []list.Item

	if v.query == "" {
		v.list.SetItems([]list.Item{statusItem{text: "Type to search..."}})
		return
	}

	switch v.tab {
	case tabTracks:
		for _, t := range v.tracks {
			items = append(items, t)
		}
	case tabEpisodes:
		for _, e := range v.episodes {
			items = append(items, e)
		}
	}

	if len(items) == 0 {
		if v.tabPending() > 0 {
			items = []list.Item{statusItem{text: "Loading..."}}
		} else if v.searchErr != nil {
			items = []list.Item{statusItem{
				text:    fmt.Sprintf("Search failed: %v", v.searchErr),
				isError: true,
			}}
		} else {
			items = []list.Item{statusItem{text: "No results"}}
		}
	}

	v.list.SetItems(items)
	if prev < len(items) {
		v.list.Select(prev)
	}

	// Resolve deferred sync after new items are added.
	if v.syncURI != "" {
		for i, item := range items {
			if u, ok := item.(interface{ URI() string }); ok && u.URI() == v.syncURI {
				v.list.Select(i)
				v.syncURI = ""
				break
			}
		}
	}
}

func (v *searchView) switchTab(tab int) {
	if v.tab == tab {
		return
	}
	v.tab = tab
	v.rebuildList()
	v.list.Select(0)
}

func (v *searchView) selectByURI(uri string) bool {
	// Search in the active tab's list items.
	for i, item := range v.list.Items() {
		if u, ok := item.(interface{ URI() string }); ok && u.URI() == uri {
			v.list.Select(i)
			v.syncURI = ""
			return false
		}
	}
	// Check if it exists in the non-active tab (no need to fetch more).
	switch v.tab {
	case tabTracks:
		for _, e := range v.episodes {
			if e.uri == uri {
				return false
			}
		}
	case tabEpisodes:
		for _, t := range v.tracks {
			if t.uri == uri {
				return false
			}
		}
	}
	v.syncURI = uri
	return v.tabPending() == 0 && (v.trackHasMore || v.episodeHasMore)
}

const maxQueueURIs = 50

func (v searchView) trackQueueFrom(uri string) []string {
	var uris []string
	found := false
	for _, t := range v.tracks {
		if t.uri == uri {
			found = true
		}
		if found {
			uris = append(uris, t.uri)
			if len(uris) >= maxQueueURIs {
				break
			}
		}
	}
	return uris
}

func (v searchView) episodeQueueFrom(uri string) []string {
	var uris []string
	found := false
	for _, e := range v.episodes {
		if e.uri == uri {
			found = true
		}
		if found {
			uris = append(uris, e.uri)
			if len(uris) >= maxQueueURIs {
				break
			}
		}
	}
	return uris
}

func (v searchView) Update(msg tea.Msg) (searchView, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		if msg.seq != v.debounceSeq {
			return v, nil
		}
		v.query = msg.query
		v.tracks = nil
		v.episodes = nil
		v.trackOffset = 0
		v.episodeOffset = 0
		v.trackHasMore = false
		v.episodeHasMore = false
		v.trackPending = 1
		v.episodePending = 1
		v.searchErr = nil
		v.list.SetItems([]list.Item{statusItem{text: "Loading..."}})
		return v, tea.Batch(v.fetchTracks(msg.query, 0, 10), v.fetchEpisodes(msg.query, 0, 10))

	case searchTracksMsg:
		if msg.query != v.query {
			return v, nil
		}
		v.trackPending--
		if msg.err != nil {
			v.searchErr = msg.err
			v.trackHasMore = false
		} else {
			for _, t := range msg.tracks {
				v.tracks = append(v.tracks, trackItem{
					uri: t.URI, name: t.Name,
					artist: t.Artist, album: t.Album, duration: t.Duration,
				})
			}
			v.trackOffset += len(msg.tracks)
			v.trackHasMore = msg.hasMore
		}
		if v.tab == tabTracks {
			v.rebuildList()
			if fetchCmds := v.resolveSyncFetch(); len(fetchCmds) > 0 {
				return v, tea.Batch(fetchCmds...)
			}
		}
		return v, nil

	case searchEpisodesMsg:
		if msg.query != v.query {
			return v, nil
		}
		v.episodePending--
		if msg.err != nil {
			v.searchErr = msg.err
			v.episodeHasMore = false
		} else {
			for _, e := range msg.episodes {
				v.episodes = append(v.episodes, episodeItem{
					uri: e.URI, name: e.Name,
					releaseDate: e.ReleaseDate, duration: e.Duration,
				})
			}
			v.episodeOffset += len(msg.episodes)
			v.episodeHasMore = msg.hasMore
		}
		if v.tab == tabEpisodes {
			v.rebuildList()
			if fetchCmds := v.resolveSyncFetch(); len(fetchCmds) > 0 {
				return v, tea.Batch(fetchCmds...)
			}
		}
		return v, nil
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	cmds := []tea.Cmd{cmd}

	// Pagination: fetch more when scrolling near the bottom of the active tab.
	if v.tabPending() == 0 && v.query != "" {
		var totalItems int
		switch v.tab {
		case tabTracks:
			totalItems = len(v.tracks)
		case tabEpisodes:
			totalItems = len(v.episodes)
		}
		if totalItems > 0 && totalItems-v.list.Index() <= 10 {
			cmds = append(cmds, v.fetchMore()...)
		}
	}

	return v, tea.Batch(cmds...)
}

func (v searchView) renderTabs() string {
	labels := []string{"Tracks", "Episodes"}
	var parts []string
	for i, label := range labels {
		if i == v.tab {
			parts = append(parts, searchTabActiveStyle.Render(label))
		} else {
			parts = append(parts, searchTabInactiveStyle.Render(label))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return lipgloss.NewStyle().MarginLeft(2).Render(row)
}

func (v searchView) View() string {
	return v.renderTabs() + "\n" + v.list.View()
}
