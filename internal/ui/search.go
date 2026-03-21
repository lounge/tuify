package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type sectionItem struct {
	title string
}

func (i sectionItem) Title() string       { return sectionStyle.Render(i.title) }
func (i sectionItem) Description() string { return "" }
func (i sectionItem) FilterValue() string { return "" }

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
	pendingLoads   int
	syncURI        string // deferred URI to select after more items load
	searchErr      error  // last search error, shown when no results
}

func newSearchView(client *spotify.Client, width, height int) searchView {
	l := newList(width, height)
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

// resolveSyncFetch triggers more fetches if syncURI is still pending after a rebuild.
func (v *searchView) resolveSyncFetch() []tea.Cmd {
	if v.syncURI == "" || v.pendingLoads > 0 {
		return nil
	}
	return v.fetchMore()
}

func (v *searchView) fetchMore() []tea.Cmd {
	var cmds []tea.Cmd
	if v.trackHasMore {
		v.pendingLoads++
		cmds = append(cmds, v.fetchTracks(v.query, v.trackOffset, 10))
	}
	if v.episodeHasMore {
		v.pendingLoads++
		cmds = append(cmds, v.fetchEpisodes(v.query, v.episodeOffset, 10))
	}
	return cmds
}

func (v *searchView) rebuildList() {
	prev := v.list.Index()
	var items []list.Item
	if len(v.tracks) > 0 {
		items = append(items, sectionItem{title: "Tracks"})
		for _, t := range v.tracks {
			items = append(items, t)
		}
	}
	if len(v.episodes) > 0 {
		items = append(items, sectionItem{title: "Episodes"})
		for _, e := range v.episodes {
			items = append(items, e)
		}
	}
	if len(items) == 0 && v.pendingLoads == 0 {
		if v.searchErr != nil {
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
	if _, ok := v.list.SelectedItem().(sectionItem); ok {
		v.skipSectionHeaders(1)
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

// skipSectionHeaders moves the cursor past any sectionItem in the given direction.
func (v *searchView) skipSectionHeaders(dir int) {
	for {
		sel := v.list.SelectedItem()
		if sel == nil {
			return
		}
		if _, ok := sel.(sectionItem); !ok {
			return
		}
		prev := v.list.Index()
		if dir > 0 {
			v.list.CursorDown()
		} else {
			v.list.CursorUp()
		}
		if v.list.Index() == prev {
			return
		}
	}
}

func (v *searchView) selectByURI(uri string) bool {
	for i, item := range v.list.Items() {
		if u, ok := item.(interface{ URI() string }); ok && u.URI() == uri {
			v.list.Select(i)
			v.syncURI = ""
			return false
		}
	}
	v.syncURI = uri
	return v.pendingLoads == 0 && (v.trackHasMore || v.episodeHasMore)
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
		v.pendingLoads = 2
		v.searchErr = nil
		v.list.SetItems([]list.Item{statusItem{text: "Loading..."}})
		return v, tea.Batch(v.fetchTracks(msg.query, 0, 10), v.fetchEpisodes(msg.query, 0, 10))

	case searchTracksMsg:
		if msg.query != v.query {
			return v, nil
		}
		v.pendingLoads--
		if msg.err != nil {
			v.searchErr = msg.err
			v.trackHasMore = false
		} else {
			for _, t := range msg.tracks {
				v.tracks = append(v.tracks, trackItem{
					id: t.ID, uri: t.URI, name: t.Name,
					artist: t.Artist, album: t.Album, duration: t.Duration,
				})
			}
			v.trackOffset += len(msg.tracks)
			v.trackHasMore = msg.hasMore
		}
		v.rebuildList()
		if fetchCmds := v.resolveSyncFetch(); len(fetchCmds) > 0 {
			return v, tea.Batch(fetchCmds...)
		}
		return v, nil

	case searchEpisodesMsg:
		if msg.query != v.query {
			return v, nil
		}
		v.pendingLoads--
		if msg.err != nil {
			v.searchErr = msg.err
			v.episodeHasMore = false
		} else {
			for _, e := range msg.episodes {
				v.episodes = append(v.episodes, episodeItem{
					id: e.ID, uri: e.URI, name: e.Name,
					releaseDate: e.ReleaseDate, duration: e.Duration,
				})
			}
			v.episodeOffset += len(msg.episodes)
			v.episodeHasMore = msg.hasMore
		}
		v.rebuildList()
		if fetchCmds := v.resolveSyncFetch(); len(fetchCmds) > 0 {
			return v, tea.Batch(fetchCmds...)
		}
		return v, nil
	}

	prevIdx := v.list.Index()
	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	cmds := []tea.Cmd{cmd}

	// Skip section headers when navigating
	if idx := v.list.Index(); idx != prevIdx {
		dir := 1
		if idx < prevIdx {
			dir = -1
		}
		v.skipSectionHeaders(dir)
	}

	// Pagination: fetch more when scrolling near the bottom
	if v.pendingLoads == 0 && v.query != "" {
		totalItems := len(v.tracks) + len(v.episodes)
		if totalItems > 0 {
			headers := 0
			if len(v.tracks) > 0 {
				headers++
			}
			if len(v.episodes) > 0 {
				headers++
			}
			if totalItems+headers-v.list.Index() <= 10 {
				if v.episodeHasMore {
					v.pendingLoads++
					cmds = append(cmds, v.fetchEpisodes(v.query, v.episodeOffset, 10))
				} else if v.trackHasMore {
					v.pendingLoads++
					cmds = append(cmds, v.fetchTracks(v.query, v.trackOffset, 10))
				}
			}
		}
	}

	return v, tea.Batch(cmds...)
}

func (v searchView) View() string {
	return v.list.View()
}
