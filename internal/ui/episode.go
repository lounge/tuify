package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type episodeItem struct {
	uri         string
	name        string
	releaseDate string
	duration    time.Duration
}

func (i episodeItem) Title() string { return i.name }
func (i episodeItem) Description() string {
	return fmt.Sprintf("%s · %s", i.releaseDate, formatDuration(i.duration))
}
func (i episodeItem) FilterValue() string { return i.name }
func (i episodeItem) URI() string         { return i.uri }

type episodeView struct {
	lazyList
	showID   string
	showName string
}

func newEpisodeView(ctx context.Context, client *spotify.Client, showID, showName string, width, height int, vimMode bool) *episodeView {
	load := func(ctx context.Context, offset int) ([]list.Item, int, bool, error) {
		episodes, rawCount, hasMore, err := client.GetShowEpisodes(ctx, showID, offset, 50)
		items := make([]list.Item, 0, len(episodes))
		for _, e := range episodes {
			items = append(items, episodeItem{
				uri: e.URI, name: e.Name,
				releaseDate: e.ReleaseDate, duration: e.Duration,
			})
		}
		return items, rawCount, hasMore, err
	}
	return &episodeView{
		lazyList: newLazyList(ctx, load, width, height, vimMode),
		showID:   showID,
		showName: showName,
	}
}

func (v *episodeView) onEnter() tea.Cmd {
	if ei, ok := v.list.SelectedItem().(episodeItem); ok {
		return emitIntent(playItemIntent{
			itemURI:    ei.uri,
			contextURI: v.contextURI(),
		})
	}
	cmd, _ := v.retryOnError()
	return cmd
}

func (v *episodeView) breadcrumb() string {
	return fmt.Sprintf("Home > Podcasts > %s", v.showName)
}

// contextURI is the playback context the episodes in this view play from.
func (v *episodeView) contextURI() string {
	return "spotify:show:" + v.showID
}

// syncTo implements syncableView: pages for the playing episode only when
// it plays from this show.
func (v *episodeView) syncTo(uri, contextURI string) tea.Cmd {
	return v.syncSelection(uri, contextURI == v.contextURI())
}
