package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type podcastItem struct {
	id           string
	uri          string
	name         string
	episodeCount int
}

func (i podcastItem) Title() string       { return i.name }
func (i podcastItem) Description() string { return fmt.Sprintf("%d episodes", i.episodeCount) }
func (i podcastItem) FilterValue() string { return i.name }
func (i podcastItem) URI() string         { return i.uri }

type podcastView struct {
	lazyList
}

func newPodcastView(ctx context.Context, client *spotify.Client, width, height int, vimMode bool) *podcastView {
	load := func(ctx context.Context, offset int) ([]list.Item, int, bool, error) {
		shows, hasMore, err := client.GetSavedShows(ctx, offset, 50)
		items := make([]list.Item, 0, len(shows))
		for _, s := range shows {
			items = append(items, podcastItem{
				id: s.ID, uri: s.URI, name: s.Name, episodeCount: s.TotalEpisodes,
			})
		}
		return items, len(shows), hasMore, err
	}
	return &podcastView{lazyList: newLazyList(ctx, load, width, height, vimMode)}
}

func (v *podcastView) onEnter() tea.Cmd {
	if pi, ok := v.list.SelectedItem().(podcastItem); ok {
		return emitIntent(openEpisodesIntent{showID: pi.id, showName: pi.name})
	}
	cmd, _ := v.retryOnError()
	return cmd
}

func (v *podcastView) breadcrumb() string { return "Home > Podcasts" }
