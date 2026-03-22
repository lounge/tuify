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

func newTrackView(client *spotify.Client, playlistID, playlistName string, width, height int) trackView {
	return trackView{
		lazyList:     newLazyList(width, height),
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

func (v trackView) Update(msg tea.Msg) (trackView, tea.Cmd) {
	switch msg := msg.(type) {
	case tracksLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return v, nil
		}
		var items []list.Item
		for _, t := range msg.tracks {
			items = append(items, trackItem{
				uri: t.URI, name: t.Name,
				artist: t.Artist, album: t.Album, duration: t.Duration,
			})
		}
		if v.append(items, len(msg.tracks), msg.hasMore) {
			return v, v.fetchMore()
		}
		if v.resolveSync() {
			return v, v.fetchMore()
		}
		return v, nil
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	cmds := []tea.Cmd{cmd}

	if v.triggerLoad() {
		cmds = append(cmds, v.fetchMore())
	}

	return v, tea.Batch(cmds...)
}

func (v *trackView) retryLoad() tea.Cmd {
	v.lazyList.retryLoad()
	return v.fetchMore()
}

func (v trackView) View() string {
	return v.list.View()
}
