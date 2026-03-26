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

type podcastsLoadedMsg struct {
	shows   []spotify.Show
	hasMore bool
	err     error
}

type podcastView struct {
	lazyList
	client *spotify.Client
}

func newPodcastView(client *spotify.Client, width, height int, vimMode bool) *podcastView {
	return &podcastView{
		lazyList: newLazyList(width, height, vimMode),
		client:   client,
	}
}

func (v podcastView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v podcastView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	return func() tea.Msg {
		shows, hasMore, err := client.GetSavedShows(context.Background(), offset, 50)
		return podcastsLoadedMsg{shows: shows, hasMore: hasMore, err: err}
	}
}

func (v *podcastView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case podcastsLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return nil
		}
		var items []list.Item
		for _, s := range msg.shows {
			items = append(items, podcastItem{
				id: s.ID, uri: s.URI, name: s.Name, episodeCount: s.TotalEpisodes,
			})
		}
		v.append(items, len(msg.shows), msg.hasMore)
		return nil
	}

	return v.updateList(msg, v.fetchMore)
}

func (v *podcastView) OnEnter(m *Model) tea.Cmd {
	selected := v.list.SelectedItem()
	if pi, ok := selected.(podcastItem); ok {
		ev := newEpisodeView(m.client, pi.id, pi.name, m.width, m.listHeight(), m.vimMode)
		m.pushView(ev)
		return ev.Init()
	}
	if si, ok := selected.(statusItem); ok && si.isError {
		return v.retryLoad()
	}
	return nil
}

func (v *podcastView) retryLoad() tea.Cmd {
	v.prepareRetry()
	return v.fetchMore()
}

func (v *podcastView) Breadcrumb() string { return "Home > Podcasts" }
