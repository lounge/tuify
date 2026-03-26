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

func newEpisodeView(client *spotify.Client, showID, showName string, width, height int, vimMode bool) *episodeView {
	return &episodeView{
		lazyList: newLazyList(width, height, vimMode),
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

func (v *episodeView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case episodesLoadedMsg:
		v.onLoaded()
		if msg.err != nil {
			v.onError(msg.err)
			return nil
		}
		var items []list.Item
		for _, e := range msg.episodes {
			items = append(items, episodeItem{
				uri: e.URI, name: e.Name,
				releaseDate: e.ReleaseDate, duration: e.Duration,
			})
		}
		if v.append(items, len(msg.episodes), msg.hasMore) {
			return v.fetchMore()
		}
		if v.resolveSync() {
			return v.fetchMore()
		}
		return nil
	}

	return v.updateList(msg, v.fetchMore)
}

func (v *episodeView) OnEnter(m *Model) tea.Cmd {
	selected := v.list.SelectedItem()
	if ei, ok := selected.(episodeItem); ok {
		return m.playItem(ei.uri, "spotify:show:"+v.showID)
	}
	if si, ok := selected.(statusItem); ok && si.isError {
		return v.retryLoad()
	}
	return nil
}

func (v *episodeView) retryLoad() tea.Cmd {
	v.prepareRetry()
	return v.fetchMore()
}

func (v *episodeView) Breadcrumb() string {
	return fmt.Sprintf("Home > Podcasts > %s", v.showName)
}

func (v *episodeView) SyncURI(uri string) tea.Cmd {
	if v.selectByURI(uri) {
		return v.fetchMore()
	}
	return nil
}

func (v *episodeView) SearchableList() *lazyList { return &v.lazyList }
func (v *episodeView) FetchMore() tea.Cmd         { return v.fetchMore() }
