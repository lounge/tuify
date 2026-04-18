package ui

import tea "github.com/charmbracelet/bubbletea"

// Intent messages emitted by views to request shell-level actions.
// Views don't know how the shell fulfills them — they just say "open the
// tracks of playlist X" or "play this URI" and the Model.Update switch
// decides how (construct and push a view, dispatch a command, etc).
//
// One-way dependency: views → shell, never shell → view internals.

// openSearchIntent opens the dedicated search view.
type openSearchIntent struct{}

// openPlaylistsIntent opens the user's playlists list.
type openPlaylistsIntent struct{}

// openPodcastsIntent opens the user's saved-shows list.
type openPodcastsIntent struct{}

// openTracksIntent opens a track list for a given playlist.
type openTracksIntent struct {
	playlistID   string
	playlistName string
}

// openEpisodesIntent opens an episode list for a given podcast show.
type openEpisodesIntent struct {
	showID   string
	showName string
}

// playItemIntent requests playback of a single item, optionally within a
// container context (playlist/album/show) so Next/Previous navigate within it.
type playItemIntent struct {
	itemURI    string
	contextURI string // empty = standalone play, not part of a context
}

// playQueueIntent requests playback of an explicit ordered URI list.
type playQueueIntent struct {
	uris []string
}

// emitIntent wraps an intent message in a tea.Cmd so OnEnter/playSelected
// can return it as their Cmd result. Bubbletea will deliver the message
// to Model.Update on the next tick.
func emitIntent(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
