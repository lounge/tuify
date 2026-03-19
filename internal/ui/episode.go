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
	id          string
	uri         string
	name        string
	releaseDate string
	duration    time.Duration
}

func (i episodeItem) Title() string       { return i.name }
func (i episodeItem) Description() string { return fmt.Sprintf("%s · %s", i.releaseDate, formatDuration(i.duration)) }
func (i episodeItem) FilterValue() string { return i.name }

type episodesLoadedMsg struct {
	episodes []spotify.Episode
	hasMore  bool
	err      error
}

type episodeView struct {
	list     list.Model
	client   *spotify.Client
	showID   string
	showName string
	items    []list.Item
	offset   int
	loading  bool
	hasMore  bool
	syncURI  string
}

func newEpisodeView(client *spotify.Client, showID, showName string, width, height int) episodeView {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	return episodeView{
		list:     l,
		client:   client,
		showID:   showID,
		showName: showName,
		loading:  true,
		hasMore:  true,
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
		v.loading = false
		v.items = removeStatusItems(v.items)
		if msg.err != nil {
			v.items = append(v.items, statusItem{text: fmt.Sprintf("Failed to load: %v — press Enter to retry", msg.err), isError: true})
			v.list.SetItems(v.items)
			return v, nil
		}
		for _, e := range msg.episodes {
			v.items = append(v.items, episodeItem{
				id: e.ID, uri: e.URI, name: e.Name,
				releaseDate: e.ReleaseDate, duration: e.Duration,
			})
		}
		v.offset += len(msg.episodes)
		v.hasMore = msg.hasMore
		v.list.SetItems(v.items)

		if v.syncURI != "" {
			if i, ok := v.findEpisode(v.syncURI); ok {
				v.list.Select(i)
				v.syncURI = ""
			} else if v.hasMore {
				v.loading = true
				return v, v.fetchMore()
			} else {
				v.syncURI = ""
			}
		}
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

func (v episodeView) View() string {
	return v.list.View()
}

func (v *episodeView) selectEpisode(uri string) tea.Cmd {
	if i, ok := v.findEpisode(uri); ok {
		v.list.Select(i)
		v.syncURI = ""
		return nil
	}
	v.syncURI = uri
	if v.hasMore && !v.loading {
		v.loading = true
		return v.fetchMore()
	}
	return nil
}

func (v episodeView) findEpisode(uri string) (int, bool) {
	for i, item := range v.items {
		if ei, ok := item.(episodeItem); ok && ei.uri == uri {
			return i, true
		}
	}
	return 0, false
}
