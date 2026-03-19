package ui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type homeItem struct {
	name string
	kind viewKind
}

func (i homeItem) Title() string       { return i.name }
func (i homeItem) Description() string { return "" }
func (i homeItem) FilterValue() string { return i.name }

type homeView struct {
	list list.Model
}

func newHomeView(width, height int) homeView {
	items := []list.Item{
		homeItem{name: "Playlists", kind: viewPlaylists},
		homeItem{name: "Podcasts", kind: viewPodcasts},
	}
	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

	return homeView{list: l}
}

func (v homeView) Update(msg tea.Msg) (homeView, tea.Cmd) {
	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	return v, cmd
}

func (v homeView) View() string {
	return v.list.View()
}
