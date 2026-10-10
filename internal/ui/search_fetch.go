package ui

import (
	"context"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// fetchCmd builds a tea.Cmd that fetches data, converts each result to a list.Item,
// and wraps the outcome in a searchResultMsg.
func fetchCmd[T any](
	parent context.Context, epoch uint64, term string,
	fetch func(ctx context.Context) ([]T, bool, error),
	convert func(T) list.Item,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, listFetchTimeout)
		defer cancel()
		results, hasMore, err := fetch(ctx)
		var items []list.Item
		for _, r := range results {
			items = append(items, convert(r))
		}
		return searchResultMsg{items: items, hasMore: hasMore, query: term, epoch: epoch, err: err}
	}
}

// searchPageSize is how many results each search or drill-down page asks
// Spotify for.
const searchPageSize = 10

// searchFetchAhead is how many loaded rows may remain below the cursor
// before the next page is requested. It is strictly less than
// searchPageSize: at the page size a fresh page with the cursor on its
// first row would already qualify, and every search would cost a second
// request before the user moved.
const searchFetchAhead = 5

func (v searchView) fetchResults(term string, offset int) tea.Cmd {
	const limit = searchPageSize
	client := v.client
	prefix := v.prefix
	depth := v.depth
	epoch := v.epoch
	parent := v.ctx

	// depth > 0: fetch detail items for a selected container
	if depth == 1 && prefix == prefixArtist {
		artistID := v.selectedArtist.id
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Album, bool, error) {
				return client.GetArtistAlbums(ctx, artistID, offset, limit)
			},
			func(a spotify.Album) list.Item {
				return albumItem{id: a.ID, uri: a.URI, name: a.Name, artist: a.Artist, releaseDate: a.ReleaseDate, trackCount: a.TrackCount}
			},
		)
	}
	if (depth == 1 && prefix == prefixAlbum) || (depth == 2 && prefix == prefixArtist) {
		albumID := v.selectedAlbum.id
		albumName := v.selectedAlbum.name
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Track, bool, error) {
				return client.GetAlbumTracks(ctx, albumID, offset, limit)
			},
			func(t spotify.Track) list.Item {
				return trackItem{uri: t.URI, name: t.Name, artist: t.Artist, album: albumName, duration: t.Duration}
			},
		)
	}
	if depth == 1 && prefix == prefixShow {
		showID := v.selectedShow.id
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Episode, bool, error) {
				return client.GetShowEpisodes(ctx, showID, offset, limit)
			},
			func(e spotify.Episode) list.Item {
				return episodeItem{uri: e.URI, name: e.Name, releaseDate: e.ReleaseDate, duration: e.Duration}
			},
		)
	}

	// depth 0: search by prefix type
	switch prefix {
	case prefixEpisode:
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Episode, bool, error) {
				return client.SearchEpisodes(ctx, term, offset, limit)
			},
			func(e spotify.Episode) list.Item {
				return episodeItem{uri: e.URI, name: e.Name, releaseDate: e.ReleaseDate, duration: e.Duration}
			},
		)
	case prefixAlbum:
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Album, bool, error) {
				return client.SearchAlbums(ctx, term, offset, limit)
			},
			func(a spotify.Album) list.Item {
				return albumItem{id: a.ID, uri: a.URI, name: a.Name, artist: a.Artist, releaseDate: a.ReleaseDate, trackCount: a.TrackCount}
			},
		)
	case prefixArtist:
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Artist, bool, error) {
				return client.SearchArtists(ctx, term, offset, limit)
			},
			func(a spotify.Artist) list.Item {
				return artistItem{id: a.ID, uri: a.URI, name: a.Name, genres: a.Genres}
			},
		)
	case prefixShow:
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Show, bool, error) {
				return client.SearchShows(ctx, term, offset, limit)
			},
			func(s spotify.Show) list.Item {
				return podcastItem{id: s.ID, uri: s.URI, name: s.Name, episodeCount: s.TotalEpisodes}
			},
		)
	default: // prefixTrack
		return fetchCmd(parent, epoch, term,
			func(ctx context.Context) ([]spotify.Track, bool, error) {
				return client.SearchTracks(ctx, term, offset, limit)
			},
			func(t spotify.Track) list.Item {
				return trackItem{uri: t.URI, name: t.Name, artist: t.Artist, album: t.Album, duration: t.Duration}
			},
		)
	}
}

// fetchMore requests the next page. Nothing is fetched while a page is in
// flight, past the last page, or after a failed page: that one is retried
// from its error row (retry), not on the next cursor move.
func (v *searchView) fetchMore() tea.Cmd {
	if v.hasMore && v.pending == 0 && v.searchErr == nil {
		v.pending++
		term := v.query
		if v.depth > 0 {
			term = "" // detail fetches don't need the search term
		}
		return v.fetchResults(term, v.offset)
	}
	return nil
}

// goBackFetchCmd returns the fetch command needed after goBack.
func (v *searchView) goBackFetchCmd() tea.Cmd {
	if v.pending > 0 {
		return v.fetchResults(v.query, 0)
	}
	return nil
}

// retry re-triggers the failed search, detail or page fetch. Results
// loaded before the failure stay on screen; only the error row gives way
// to a loading row until the page arrives.
func (v *searchView) retry() tea.Cmd {
	v.searchErr = nil
	v.pending = 1
	v.rebuildList()
	term := v.query
	if v.depth > 0 {
		term = ""
	}
	return v.fetchResults(term, v.offset)
}

// rebuildList refreshes v.list from v.items, swapping in loading/error/empty
// placeholder rows when there are no real items yet. When there are, a
// failed or retried later page shows as an error or loading row after
// them, as lazyList does, so the failure is visible and Enter can retry
// it.
func (v *searchView) rebuildList() {
	prev := v.list.Index()
	items := v.items
	errRow := statusItem{
		text:    "Search failed: " + userMessage(v.searchErr),
		desc:    "press Enter to retry",
		isError: true,
	}
	switch {
	case len(items) == 0:
		switch {
		case v.pending > 0:
			items = []list.Item{loadingStatusItem}
		case v.searchErr != nil:
			items = []list.Item{errRow}
		case v.query == "" && v.depth == 0:
			items = nil
		default:
			items = []list.Item{statusItem{text: "No results"}}
		}
	case v.pending > 0:
		items = appendRow(items, statusItem{text: "Loading more…", spinning: true})
	case v.searchErr != nil:
		items = appendRow(items, errRow)
	}

	// Keep the cursor row across the rebuild, clamped to the new length:
	// bubbles' SetItems clamps the page but not the cursor, so a cursor
	// left past the end of a shorter result set would select nothing (no
	// highlighted row, Enter a no-op) until the user moved it.
	v.list.ResetSelected()
	v.list.SetItems(items)
	if n := len(items); n > 0 {
		v.list.Select(min(prev, n-1))
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

// appendRow returns items plus row in a fresh slice, so the status row is
// never written into the backing array of v.items.
func appendRow(items []list.Item, row list.Item) []list.Item {
	out := make([]list.Item, 0, len(items)+1)
	out = append(out, items...)
	return append(out, row)
}
