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

func (i episodeItem) Title() string       { return i.name }
func (i episodeItem) Description() string { return fmt.Sprintf("%s · %s", i.releaseDate, formatDuration(i.duration)) }
func (i episodeItem) FilterValue() string { return i.name }
func (i episodeItem) URI() string          { return i.uri }

type episodesLoadedMsg struct {
	episodes []spotify.Episode
	hasMore  bool
	err      error
}

type episodeView struct {
	lazyList
	client   *spotify.Client
	showID   string
	showName string
}

func newEpisodeView(client *spotify.Client, showID, showName string, width, height int) episodeView {
	return episodeView{
		lazyList: newLazyList(width, height),
		client:   client,
		showID:   showID,
		showName: showName,
	}
}

func (v episodeView) Init() tea.Cmd {
	return v.fetchMore()
}

func (v episodeView) fetchMore() tea.Cmd {
	offset := v.offset
	client := v.client
	showID := v.showID
	return func() tea.Msg {
		episodes, hasMore, err := client.GetShowEpisodes(context.Background(), showID, offset, 50)
		return episodesLoadedMsg{episodes: episodes, hasMore: hasMore, err: err}
	}
}

func (v episodeView) Update(msg tea.Msg) (episodeView, tea.Cmd) {
	switch msg := msg.(type) {
	case episodesLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return v, nil
		}
		var items []list.Item
		for _, e := range msg.episodes {
			items = append(items, episodeItem{
				uri: e.URI, name: e.Name,
				releaseDate: e.ReleaseDate, duration: e.Duration,
			})
		}
		if v.append(items, len(msg.episodes), msg.hasMore) {
			return v, v.fetchMore()
		}
		if v.resolveSync() {
			return v, v.fetchMore()
		}
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

func (v *episodeView) retryLoad() tea.Cmd {
	v.lazyList.retryLoad()
	return v.fetchMore()
}

func (v episodeView) View() string {
	return v.list.View()
}
