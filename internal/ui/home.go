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
	{name: "Playlists", kind: viewPlaylists},
	{name: "Podcasts", kind: viewPodcasts},
}

type homeView struct {
	cursor int
	width  int
	height int
}

func newHomeView(width, height int) homeView {
	return homeView{width: width, height: height}
}

func (v homeView) Update(msg tea.Msg) (homeView, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "left", "h":
			if v.cursor > 0 {
				v.cursor--
			}
		case "right", "l":
			if v.cursor < len(homeItems)-1 {
				v.cursor++
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

	row := lipgloss.JoinHorizontal(lipgloss.Center, tabs...)
	return lipgloss.Place(v.width, v.height, lipgloss.Center, lipgloss.Center, row)
}
