package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type trackItem struct {
	uri      string
	name     string
	artist   string
	album    string
	duration time.Duration
}

func (i trackItem) Title() string { return i.name }
func (i trackItem) Description() string {
	return fmt.Sprintf("%s · %s · %s", i.artist, i.album, formatDuration(i.duration))
}
func (i trackItem) FilterValue() string { return i.name }
func (i trackItem) URI() string         { return i.uri }

type tracksLoadedMsg struct {
	tracks  []spotify.Track
	hasMore bool
	err     error
}

type trackView struct {
	lazyList
	client       *spotify.Client
	playlistID   string
	playlistName string
}

func newTrackView(client *spotify.Client, playlistID, playlistName string, width, height int, vimMode bool) *trackView {
	return &trackView{
		lazyList:     newLazyList(width, height, vimMode),
		client:       client,
		playlistID:   playlistID,
		playlistName: playlistName,
	}
}

func (v trackView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v trackView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	playlistID := v.playlistID
	return func() tea.Msg {
		tracks, hasMore, err := client.GetPlaylistTracks(context.Background(), playlistID, offset, 50)
		return tracksLoadedMsg{tracks: tracks, hasMore: hasMore, err: err}
	}
}

func (v *trackView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tracksLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return nil
		}
		var items []list.Item
		for _, t := range msg.tracks {
			items = append(items, trackItem{
				uri: t.URI, name: t.Name,
				artist: t.Artist, album: t.Album, duration: t.Duration,
			})
		}
		if v.append(items, len(msg.tracks), msg.hasMore) {
			return v.fetchMore()
		}
		if v.resolveSync() {
			return v.fetchMore()
		}
		return nil
	}

	return v.updateList(msg, v.fetchMore)
}

func (v *trackView) retryLoad() tea.Cmd {
	v.prepareRetry()
	return v.fetchMore()
}

func (v *trackView) Breadcrumb() string {
	return fmt.Sprintf("Home > Playlists > %s", v.playlistName)
}

func (v *trackView) SyncURI(uri string) tea.Cmd {
	if v.selectByURI(uri) {
		return v.fetchMore()
	}
	return nil
}

func (v *trackView) SearchableList() *lazyList { return &v.lazyList }
func (v *trackView) FetchMore() tea.Cmd         { return v.fetchMore() }
