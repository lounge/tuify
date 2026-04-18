package ui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

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

func (v *searchView) OnEnter(m *Model) tea.Cmd {
	selected := v.list.SelectedItem()
	if si, ok := selected.(statusItem); ok && si.isError {
		return v.retry()
	}
	if v.isPlayable() {
		return v.playSelected(m, selected)
	}
	return v.drillDown(selected)
}

func (v *searchView) playSelected(m *Model, item list.Item) tea.Cmd {
	ctx := v.contextURI()
	if ctx != "" {
		if ti, ok := item.(trackItem); ok {
			return m.playItem(ti.uri, ctx)
		}
		if ei, ok := item.(episodeItem); ok {
			return m.playItem(ei.uri, ctx)
		}
	}
	if ti, ok := item.(trackItem); ok {
		return m.playQueue(v.queueFrom(ti.uri))
	}
	if ei, ok := item.(episodeItem); ok {
		return m.playQueue(v.queueFrom(ei.uri))
	}
	return nil
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
