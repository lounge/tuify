package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

// searchPrefix identifies the type of search.
type searchPrefix int

const (
	prefixTrack   searchPrefix = iota // default / "t:"
	prefixEpisode                     // "e:"
	prefixAlbum                       // "l:"
	prefixArtist                      // "a:"
	prefixShow                        // "s:"
)

// parseSearch splits input into prefix + term. Returns prefixTrack for
// unrecognised prefixes (the whole string becomes the term).
func parseSearch(input string) (searchPrefix, string) {
	if idx := strings.Index(input, ":"); idx > 0 {
		switch input[:idx] {
		case "t":
			return prefixTrack, input[idx+1:]
		case "e":
			return prefixEpisode, input[idx+1:]
		case "l":
			return prefixAlbum, input[idx+1:]
		case "a":
			return prefixArtist, input[idx+1:]
		case "s":
			return prefixShow, input[idx+1:]
		}
	}
	return prefixTrack, input
}

// ---------- messages ----------

type searchDebounceMsg struct {
	seq   int
	query string
}

type searchResultMsg struct {
	items   []list.Item
	hasMore bool
	query   string
	epoch   int
	err     error
}

// ---------- searchView ----------

var searchHintText = strings.Join([]string{
	"t:  track search (default)",
	"e:  episode search",
	"a:  artist → album → track",
	"l:  album → track",
	"s:  show → episode",
}, "\n")

type selectedRef struct {
	id, uri, name string
}

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

func newSearchView(client *spotify.Client, width, height int) searchView {
	l := newList(width, height)
	l.SetItems(nil)
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

// ---------- fetchers ----------

// fetchCmd builds a tea.Cmd that fetches data, converts each result to a list.Item,
// and wraps the outcome in a searchResultMsg.
func fetchCmd[T any](
	epoch int, term string,
	fetch func() ([]T, bool, error),
	convert func(T) list.Item,
) tea.Cmd {
	return func() tea.Msg {
		results, hasMore, err := fetch()
		var items []list.Item
		for _, r := range results {
			items = append(items, convert(r))
		}
		return searchResultMsg{items: items, hasMore: hasMore, query: term, epoch: epoch, err: err}
	}
}

func (v searchView) fetchResults(term string, offset, limit int) tea.Cmd {
	client := v.client
	prefix := v.prefix
	depth := v.depth
	epoch := v.epoch

	// depth > 0: fetch detail items for a selected container
	if depth == 1 && prefix == prefixArtist {
		artistID := v.selectedArtist.id
		return fetchCmd(epoch, term,
			func() ([]spotify.Album, bool, error) {
				return client.GetArtistAlbums(context.Background(), artistID, offset, limit)
			},
			func(a spotify.Album) list.Item {
				return albumItem{id: a.ID, uri: a.URI, name: a.Name, artist: a.Artist, releaseDate: a.ReleaseDate, trackCount: a.TrackCount}
			},
		)
	}
	if (depth == 1 && prefix == prefixAlbum) || (depth == 2 && prefix == prefixArtist) {
		albumID := v.selectedAlbum.id
		albumName := v.selectedAlbum.name
		return fetchCmd(epoch, term,
			func() ([]spotify.Track, bool, error) {
				return client.GetAlbumTracks(context.Background(), albumID, offset, limit)
			},
			func(t spotify.Track) list.Item {
				return trackItem{uri: t.URI, name: t.Name, artist: t.Artist, album: albumName, duration: t.Duration}
			},
		)
	}
	if depth == 1 && prefix == prefixShow {
		showID := v.selectedShow.id
		return fetchCmd(epoch, term,
			func() ([]spotify.Episode, bool, error) {
				return client.GetShowEpisodes(context.Background(), showID, offset, limit)
			},
			func(e spotify.Episode) list.Item {
				return episodeItem{uri: e.URI, name: e.Name, releaseDate: e.ReleaseDate, duration: e.Duration}
			},
		)
	}

	// depth 0: search by prefix type
	switch prefix {
	case prefixEpisode:
		return fetchCmd(epoch, term,
			func() ([]spotify.Episode, bool, error) {
				return client.SearchEpisodes(context.Background(), term, offset, limit)
			},
			func(e spotify.Episode) list.Item {
				return episodeItem{uri: e.URI, name: e.Name, releaseDate: e.ReleaseDate, duration: e.Duration}
			},
		)
	case prefixAlbum:
		return fetchCmd(epoch, term,
			func() ([]spotify.Album, bool, error) {
				return client.SearchAlbums(context.Background(), term, offset, limit)
			},
			func(a spotify.Album) list.Item {
				return albumItem{id: a.ID, uri: a.URI, name: a.Name, artist: a.Artist, releaseDate: a.ReleaseDate, trackCount: a.TrackCount}
			},
		)
	case prefixArtist:
		return fetchCmd(epoch, term,
			func() ([]spotify.Artist, bool, error) {
				return client.SearchArtists(context.Background(), term, offset, limit)
			},
			func(a spotify.Artist) list.Item {
				return artistItem{id: a.ID, uri: a.URI, name: a.Name, genres: a.Genres}
			},
		)
	case prefixShow:
		return fetchCmd(epoch, term,
			func() ([]spotify.Show, bool, error) {
				return client.SearchShows(context.Background(), term, offset, limit)
			},
			func(s spotify.Show) list.Item {
				return podcastItem{id: s.ID, uri: s.URI, name: s.Name, episodeCount: s.TotalEpisodes}
			},
		)
	default: // prefixTrack
		return fetchCmd(epoch, term,
			func() ([]spotify.Track, bool, error) {
				return client.SearchTracks(context.Background(), term, offset, limit)
			},
			func(t spotify.Track) list.Item {
				return trackItem{uri: t.URI, name: t.Name, artist: t.Artist, album: t.Album, duration: t.Duration}
			},
		)
	}
}

func (v *searchView) fetchMore() tea.Cmd {
	if v.hasMore && v.pending == 0 {
		v.pending++
		term := v.query
		if v.depth > 0 {
			term = "" // detail fetches don't need the search term
		}
		return v.fetchResults(term, v.offset, 10)
	}
	return nil
}

// ---------- list management ----------

func (v *searchView) rebuildList() {
	prev := v.list.Index()
	items := v.items
	if len(items) == 0 {
		if v.pending > 0 {
			items = []list.Item{loadingStatusItem}
		} else if v.searchErr != nil {
			items = []list.Item{statusItem{
				text:    fmt.Sprintf("Search failed: %v", v.searchErr),
				isError: true,
			}}
		} else if v.query == "" && v.depth == 0 {
			items = nil
		} else {
			items = []list.Item{statusItem{text: "No results"}}
		}
	}

	v.list.SetItems(items)
	if prev < len(items) {
		v.list.Select(prev)
	}

	if v.syncURI != "" {
		for i, item := range items {
			if u, ok := item.(uriItem); ok && u.URI() == v.syncURI {
				v.list.Select(i)
				v.syncURI = ""
				break
			}
		}
	}
}

func (v *searchView) selectByURI(uri string) bool {
	for i, item := range v.list.Items() {
		if u, ok := item.(uriItem); ok && u.URI() == uri {
			v.list.Select(i)
			v.syncURI = ""
			return false
		}
	}
	v.syncURI = uri
	return v.pending == 0 && v.hasMore
}

const maxQueueURIs = 50

func (v searchView) queueFrom(uri string) []string {
	var uris []string
	found := false
	for _, item := range v.items {
		if u, ok := item.(uriItem); ok {
			if u.URI() == uri {
				found = true
			}
			if found {
				uris = append(uris, u.URI())
				if len(uris) >= maxQueueURIs {
					break
				}
			}
		}
	}
	return uris
}

// ---------- drill-down ----------

// drillDown enters a container item, returning the fetch command.
func (v *searchView) drillDown(item list.Item) tea.Cmd {
	switch v.prefix {
	case prefixAlbum:
		if ai, ok := item.(albumItem); ok {
			v.depth = 1
			v.selectedAlbum = selectedRef{id: ai.id, uri: ai.uri, name: ai.name}
			v.startLoading()
			return v.fetchResults("", 0, 10)
		}
	case prefixShow:
		if si, ok := item.(podcastItem); ok {
			v.depth = 1
			v.selectedShow = selectedRef{id: si.id, uri: si.uri, name: si.name}
			v.startLoading()
			return v.fetchResults("", 0, 10)
		}
	case prefixArtist:
		if v.depth == 0 {
			if ai, ok := item.(artistItem); ok {
				v.depth = 1
				v.selectedArtist = selectedRef{id: ai.id, name: ai.name}
				v.startLoading()
				return v.fetchResults("", 0, 10)
			}
		} else if v.depth == 1 {
			if ai, ok := item.(albumItem); ok {
				v.depth = 2
				v.selectedAlbum = selectedRef{id: ai.id, uri: ai.uri, name: ai.name}
				v.startLoading()
				return v.fetchResults("", 0, 10)
			}
		}
	}
	return nil
}

// goBack returns true if we went back a level, false if at depth 0 (caller should pop view).
func (v *searchView) goBack() bool {
	if v.depth == 0 {
		return false
	}
	if v.depth == 2 {
		// artist→album→tracks: go back to artist→albums
		v.depth = 1
		v.selectedAlbum = selectedRef{}
		v.startLoading()
		return true
	}
	// depth 1 → 0: go back to search results
	v.depth = 0
	v.selectedArtist = selectedRef{}
	v.selectedAlbum = selectedRef{}
	v.selectedShow = selectedRef{}
	v.resetPagination()
	// Re-commit the original search
	if v.query != "" {
		v.pending = 1
		v.list.SetItems([]list.Item{loadingStatusItem})
	} else {
		v.list.SetItems(nil)
	}
	return true
}

// goBackFetchCmd returns the fetch command needed after goBack.
func (v *searchView) goBackFetchCmd() tea.Cmd {
	if v.pending > 0 {
		return v.fetchResults(v.query, 0, 10)
	}
	return nil
}

// retry re-triggers the last search or detail fetch after an error.
func (v *searchView) retry() tea.Cmd {
	v.searchErr = nil
	v.pending = 1
	v.list.SetItems([]list.Item{loadingStatusItem})
	term := v.query
	if v.depth > 0 {
		term = ""
	}
	return v.fetchResults(term, v.offset, 10)
}

// isPlayable returns true if the current depth shows playable items (tracks or episodes).
func (v *searchView) isPlayable() bool {
	switch v.prefix {
	case prefixTrack, prefixEpisode:
		return true
	case prefixAlbum:
		return v.depth == 1
	case prefixShow:
		return v.depth == 1
	case prefixArtist:
		return v.depth == 2
	}
	return false
}

// contextURI returns the Spotify context URI for playback continuation.
func (v *searchView) contextURI() string {
	switch v.prefix {
	case prefixAlbum:
		if v.depth == 1 {
			return v.selectedAlbum.uri
		}
	case prefixShow:
		if v.depth == 1 {
			return v.selectedShow.uri
		}
	case prefixArtist:
		if v.depth == 2 {
			return v.selectedAlbum.uri
		}
	}
	return ""
}

// Breadcrumb returns the search-specific breadcrumb segments (after "Home > Search").
func (v *searchView) Breadcrumb() string {
	crumbs := "Home > Search"
	switch v.prefix {
	case prefixArtist:
		if v.depth >= 1 {
			crumbs += " > " + v.selectedArtist.name
		}
		if v.depth >= 2 {
			crumbs += " > " + v.selectedAlbum.name
		}
	case prefixAlbum:
		if v.depth >= 1 {
			crumbs += " > " + v.selectedAlbum.name
		}
	case prefixShow:
		if v.depth >= 1 {
			crumbs += " > " + v.selectedShow.name
		}
	}
	return crumbs
}

// ---------- Update ----------

func (v searchView) Update(msg tea.Msg) (searchView, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDebounceMsg:
		if msg.seq != v.debounceSeq {
			return v, nil
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
		return v, v.fetchResults(term, 0, 10)

	case searchResultMsg:
		if msg.epoch != v.epoch {
			return v, nil
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
			return v, v.fetchMore()
		}
		return v, nil
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

	return v, tea.Batch(cmds...)
}

func (v searchView) View() string {
	if v.depth == 0 && v.query == "" && len(v.items) == 0 && v.pending == 0 {
		box := searchHintBoxStyle.Render(searchHintText)
		return lipgloss.Place(v.list.Width(), v.list.Height(), lipgloss.Center, lipgloss.Center, box)
	}
	return v.list.View()
}
