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

type playlistView struct {
	lazyList
}

func newPlaylistView(ctx context.Context, client *spotify.Client, width, height int, vimMode bool) *playlistView {
	return &playlistView{lazyList: newLazyList(ctx, playlistLoader(client), width, height, vimMode)}
}

// playlistLoader fetches Spotify pages of 50 until at least 20 playlists
// are collected; GetPlaylists can return short pages once it filters out
// entries it can't show.
func playlistLoader(client *spotify.Client) pageLoader {
	return func(ctx context.Context, offset int) ([]list.Item, int, bool, error) {
		var items []list.Item
		fetched := 0
		hasMore := true
		for hasMore && len(items) < 20 {
			playlists, pageSize, more, err := client.GetPlaylists(ctx, offset+fetched, 50)
			if err != nil {
				return items, fetched, more, err
			}
			for _, p := range playlists {
				items = append(items, playlistItem{
					id: p.ID, name: p.Name, ownerName: p.OwnerName, trackCount: p.TrackCount,
				})
			}
			fetched += pageSize
			hasMore = more
		}
		return items, fetched, hasMore, nil
	}
}

func (v *playlistView) OnEnter() tea.Cmd {
	if pi, ok := v.list.SelectedItem().(playlistItem); ok {
		return emitIntent(openTracksIntent{playlistID: pi.id, playlistName: pi.name})
	}
	cmd, _ := v.retryOnError()
	return cmd
}

func (v *playlistView) Breadcrumb() string { return "Home > Playlists" }
