package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type homeItem struct {
	name string
}

var homeItems = []homeItem{
	{name: "Search"},
	{name: "Playlists"},
	{name: "Podcasts"},
}

type homeView struct {
	cursor  int
	width   int
	height  int
	vimMode bool
}

func newHomeView(width, height int) *homeView {
	return &homeView{width: width, height: height}
}

func (v *homeView) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "k":
			if v.cursor > 0 {
				v.cursor--
			}
		case "down", "j":
			if v.cursor < len(homeItems)-1 {
				v.cursor++
			}
		case "g":
			if v.vimMode {
				v.cursor = 0
			}
		case "G":
			if v.vimMode {
				v.cursor = len(homeItems) - 1
			}
		}
	}
	return nil
}

func (v *homeView) selectedItem() homeItem {
	return homeItems[v.cursor]
}

func (v *homeView) OnEnter(m *Model) tea.Cmd {
	switch v.selectedItem().name {
	case "Search":
		m.pushView(newSearchView(m.client, m.width, m.listHeight(), m.vimMode))
		return nil
	case "Playlists":
		pv := newPlaylistView(m.client, m.width, m.listHeight(), m.vimMode)
		m.pushView(pv)
		return pv.Init()
	case "Podcasts":
		pv := newPodcastView(m.client, m.width, m.listHeight(), m.vimMode)
		m.pushView(pv)
		return pv.Init()
	}
	return nil
}

func (v *homeView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *homeView) Breadcrumb() string { return "" }

func (v *homeView) View() string {
	var tabs []string
	for i, item := range homeItems {
		var rendered string
		if i == v.cursor {
			rendered = homeTabActive.Render(item.name)
		} else {
			rendered = homeTabInactive.Render(item.name)
		}
		// Mark each tab with its name so mouse clicks can resolve to the
		// corresponding homeItem. Item names are unique, so they double
		// as stable zone ids.
		tabs = append(tabs, zone.Mark(item.name, rendered))
	}

	column := lipgloss.JoinVertical(lipgloss.Center, tabs...)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, column)
}
