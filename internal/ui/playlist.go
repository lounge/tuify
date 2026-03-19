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
	list    list.Model
	client  *spotify.Client
	items   []list.Item
	offset  int
	loading bool
	hasMore bool
}

func newPlaylistView(client *spotify.Client, width, height int) playlistView {
	l := list.New(nil, newListDelegate(), width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	return playlistView{
		list:    l,
		client:  client,
		loading: true,
		hasMore: true,
	}
}

func (v playlistView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v playlistView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	return func() tea.Msg {
		playlists, pageSize, hasMore, err := client.GetPlaylists(context.Background(), offset, 50)
		return playlistsLoadedMsg{playlists: playlists, pageSize: pageSize, hasMore: hasMore, err: err}
	}
}

func (v playlistView) Update(msg tea.Msg) (playlistView, tea.Cmd) {
	switch msg := msg.(type) {
	case playlistsLoadedMsg:
		v.loading = false
		// Remove loading indicator
		v.items = removeStatusItems(v.items)
		if msg.err != nil {
			v.items = append(v.items, statusItem{text: fmt.Sprintf("Failed to load: %v — press Enter to retry", msg.err), isError: true})
			v.list.SetItems(v.items)
			return v, nil
		}
		for _, p := range msg.playlists {
			v.items = append(v.items, playlistItem{
				id: p.ID, name: p.Name, ownerName: p.OwnerName, trackCount: p.TrackCount,
			})
		}
		v.offset += msg.pageSize
		v.hasMore = msg.hasMore
		v.list.SetItems(v.items)
		return v, nil
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	cmds := []tea.Cmd{cmd}

	// Lazy load trigger
	if !v.loading && v.hasMore {
		idx := v.list.Index()
		if len(v.items)-idx <= 10 {
			v.loading = true
			v.items = append(v.items, statusItem{text: "Loading..."})
			v.list.SetItems(v.items)
			cmds = append(cmds, v.fetchMore())
		}
	}

	return v, tea.Batch(cmds...)
}

func (v playlistView) View() string {
	return v.list.View()
}
