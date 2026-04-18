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

func (i playlistItem) Title() string { return i.name }
func (i playlistItem) Description() string {
	return fmt.Sprintf("by %s · %d tracks", i.ownerName, i.trackCount)
}
func (i playlistItem) FilterValue() string { return i.name }
func (i playlistItem) URI() string         { return "spotify:playlist:" + i.id }

type playlistsLoadedMsg struct {
	playlists []spotify.Playlist
	pageSize  int
	hasMore   bool
	err       error
}

type playlistView struct {
	lazyList
	ctx    context.Context
	client *spotify.Client
}

func newPlaylistView(ctx context.Context, client *spotify.Client, width, height int, vimMode bool) *playlistView {
	return &playlistView{
		lazyList: newLazyList(width, height, vimMode),
		ctx:      ctx,
		client:   client,
	}
}

func (v playlistView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v playlistView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	parent := v.ctx
	return func() tea.Msg {
		var all []spotify.Playlist
		totalFetched := 0
		hasMore := true
		for hasMore && len(all) < 20 {
			playlists, pageSize, more, err := client.GetPlaylists(parent, offset, 50)
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

func (v *playlistView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case playlistsLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return nil
		}
		var items []list.Item
		for _, p := range msg.playlists {
			items = append(items, playlistItem{
				id: p.ID, name: p.Name, ownerName: p.OwnerName, trackCount: p.TrackCount,
			})
		}
		v.append(items, msg.pageSize, msg.hasMore)
		return nil
	}

	return v.updateList(msg, v.fetchMore)
}

func (v *playlistView) OnEnter(m *Model) tea.Cmd {
	selected := v.list.SelectedItem()
	if pi, ok := selected.(playlistItem); ok {
		tv := newTrackView(m.rootCtx, m.client, pi.id, pi.name, m.width, m.listHeight(), m.vimMode)
		m.pushView(tv)
		return tv.Init()
	}
	if si, ok := selected.(statusItem); ok && si.isError {
		return v.retryLoad()
	}
	return nil
}

func (v *playlistView) retryLoad() tea.Cmd {
	v.prepareRetry()
	return v.fetchMore()
}

func (v *playlistView) Breadcrumb() string { return "Home > Playlists" }
