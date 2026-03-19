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
	name         string
	episodeCount int
}

func (i podcastItem) Title() string       { return i.name }
func (i podcastItem) Description() string { return fmt.Sprintf("%d episodes", i.episodeCount) }
func (i podcastItem) FilterValue() string { return i.name }

type podcastsLoadedMsg struct {
	shows   []spotify.Show
	hasMore bool
	err     error
}

type podcastView struct {
	list    list.Model
	client  *spotify.Client
	items   []list.Item
	offset  int
	loading bool
	hasMore bool
}

func newPodcastView(client *spotify.Client, width, height int) podcastView {
	l := list.New(nil, newListDelegate(), width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	return podcastView{
		list:    l,
		client:  client,
		loading: true,
		hasMore: true,
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

func (v podcastView) Update(msg tea.Msg) (podcastView, tea.Cmd) {
	switch msg := msg.(type) {
	case podcastsLoadedMsg:
		v.loading = false
		v.items = removeStatusItems(v.items)
		if msg.err != nil {
			v.items = append(v.items, statusItem{text: fmt.Sprintf("Failed to load: %v — press Enter to retry", msg.err), isError: true})
			v.list.SetItems(v.items)
			return v, nil
		}
		for _, s := range msg.shows {
			v.items = append(v.items, podcastItem{
				id: s.ID, name: s.Name, episodeCount: s.TotalEpisodes,
			})
		}
		v.offset += len(msg.shows)
		v.hasMore = msg.hasMore
		v.list.SetItems(v.items)
		return v, nil
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	cmds := []tea.Cmd{cmd}

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

func (v podcastView) View() string {
	return v.list.View()
}
