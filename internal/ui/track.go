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

type trackView struct {
	lazyList
	playlistID   string
	playlistName string
}

func newTrackView(ctx context.Context, client *spotify.Client, playlistID, playlistName string, width, height int, vimMode bool) *trackView {
	load := func(ctx context.Context, offset int) ([]list.Item, int, bool, error) {
		// fetched is the raw page size, not len(tracks): the client drops
		// entries without a track, and advancing by the filtered count
		// would re-fetch those rows and repeat the page after them.
		tracks, fetched, hasMore, err := client.GetPlaylistTracks(ctx, playlistID, offset, 50)
		items := make([]list.Item, 0, len(tracks))
		for _, t := range tracks {
			items = append(items, trackItem{
				uri: t.URI, name: t.Name,
				artist: t.Artist, album: t.Album, duration: t.Duration,
			})
		}
		return items, fetched, hasMore, err
	}
	return &trackView{
		lazyList:     newLazyList(ctx, load, width, height, vimMode),
		playlistID:   playlistID,
		playlistName: playlistName,
	}
}

func (v *trackView) onEnter() tea.Cmd {
	if ti, ok := v.list.SelectedItem().(trackItem); ok {
		return emitIntent(playItemIntent{
			itemURI:    ti.uri,
			contextURI: v.contextURI(),
		})
	}
	cmd, _ := v.retryOnError()
	return cmd
}

func (v *trackView) breadcrumb() string {
	return fmt.Sprintf("Home > Playlists > %s", v.playlistName)
}

// contextURI is the playback context the tracks in this view play from.
func (v *trackView) contextURI() string {
	return "spotify:playlist:" + v.playlistID
}

// syncTo implements syncableView: pages for the playing track only when
// it plays from this playlist.
func (v *trackView) syncTo(uri, contextURI string) tea.Cmd {
	return v.syncSelection(uri, contextURI == v.contextURI())
}
