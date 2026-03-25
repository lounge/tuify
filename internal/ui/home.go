package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type homeItem struct {
	name string
	kind viewKind
}

var homeItems = []homeItem{
	{name: "Search", kind: viewSearch},
	{name: "Playlists", kind: viewPlaylists},
	{name: "Podcasts", kind: viewPodcasts},
}

type homeView struct {
	cursor  int
	width   int
	height  int
	vimMode bool
}

func newHomeView(width, height int) homeView {
	return homeView{width: width, height: height}
}

func (v homeView) Update(msg tea.Msg) (homeView, tea.Cmd) {
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
	return v, nil
}

func (v homeView) selectedItem() homeItem {
	return homeItems[v.cursor]
}

func (v homeView) View() string {
	var tabs []string
	for i, item := range homeItems {
		if i == v.cursor {
			tabs = append(tabs, homeTabActive.Render(item.name))
		} else {
			tabs = append(tabs, homeTabInactive.Render(item.name))
		}
	}

	column := lipgloss.JoinVertical(lipgloss.Center, tabs...)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, column)
}
