package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

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

// rebuildList refreshes v.list from v.items, swapping in loading/error/empty
// placeholder rows when there are no real items yet.
func (v *searchView) rebuildList() {
	prev := v.list.Index()
	items := v.items
	if len(items) == 0 {
		if v.pending > 0 {
			items = []list.Item{loadingStatusItem}
		} else if v.searchErr != nil {
			items = []list.Item{statusItem{
				text:    fmt.Sprintf("Search failed: %v", v.searchErr),
				desc:    "press Enter to retry",
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
