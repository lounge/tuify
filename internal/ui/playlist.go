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
	return playlistView{
		list:    newList(width, height),
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
		// Batch multiple pages in one goroutine to avoid rapid cascade
		// when owner filtering discards most results.
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
		v.loading = false
		// Remove loading indicator
		v.items = removeStatusItems(v.items)
		if msg.err != nil {
			v.hasMore = false
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

func (v *playlistView) retryLoad() tea.Cmd {
	v.hasMore = true
	v.loading = true
	v.items = removeStatusItems(v.items)
	v.items = append(v.items, statusItem{text: "Loading..."})
	v.list.SetItems(v.items)
	return v.fetchMore()
}

func (v playlistView) View() string {
	return v.list.View()
}
