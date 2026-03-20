package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type playlistItem struct {
	id         string
	name       string
	ownerName  string
	trackCount int
}

func (i playlistItem) Title() string       { return i.name }
func (i playlistItem) Description() string { return fmt.Sprintf("by %s · %d tracks", i.ownerName, i.trackCount) }
func (i playlistItem) FilterValue() string { return i.name }

type playlistsLoadedMsg struct {
	playlists []spotify.Playlist
	pageSize  int
	hasMore   bool
	err       error
}

type playlistView struct {
	lazyList
	client *spotify.Client
}

func newPlaylistView(client *spotify.Client, width, height int) playlistView {
	return playlistView{
		lazyList: newLazyList(width, height),
		client:   client,
	}
}

func (v playlistView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v playlistView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	return func() tea.Msg {
		var all []spotify.Playlist
		totalFetched := 0
		hasMore := true
		for hasMore && len(all) < 20 {
			playlists, pageSize, more, err := client.GetPlaylists(context.Background(), offset, 50)
			if err != nil {
				return playlistsLoadedMsg{playlists: all, pageSize: totalFetched, hasMore: more, err: err}
			}
			all = append(all, playlists...)
			totalFetched += pageSize
			offset += pageSize
			hasMore = more
		}
		return playlistsLoadedMsg{playlists: all, pageSize: totalFetched, hasMore: hasMore}
	}
}

func (v playlistView) Update(msg tea.Msg) (playlistView, tea.Cmd) {
	switch msg := msg.(type) {
	case playlistsLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return v, nil
		}
		var items []list.Item
		for _, p := range msg.playlists {
			items = append(items, playlistItem{
				id: p.ID, name: p.Name, ownerName: p.OwnerName, trackCount: p.TrackCount,
			})
		}
		v.append(items, msg.pageSize, msg.hasMore)
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

func (v *playlistView) retryLoad() tea.Cmd {
	v.lazyList.retryLoad()
	return v.fetchMore()
}

func (v playlistView) View() string {
	return v.list.View()
}
