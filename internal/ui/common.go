package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// view is the interface all navigable views must implement.
type view interface {
	Update(msg tea.Msg) tea.Cmd
	View() string
	SetSize(width, height int)
	Breadcrumb() string
}

// listProvider is implemented by views that expose a bubbles list.
type listProvider interface {
	List() *list.Model
}

// searchableListProvider is implemented by views that support local search.
type searchableListProvider interface {
	SearchableList() *lazyList
	FetchMore() tea.Cmd
}

// syncableView is implemented by views that sync selection to the playing track.
type syncableView interface {
	SyncURI(uri string) tea.Cmd
}

// enterable is implemented by views that handle the Enter key.
type enterable interface {
	OnEnter(m *Model) tea.Cmd
}

// uriItem is implemented by list items that have a Spotify URI.
type uriItem interface {
	URI() string
}

type statusItem struct {
	text    string
	desc    string
	isError bool
}

var loadingStatusItem = statusItem{text: "Loading..."}

func (i statusItem) Title() string {
	if i.isError {
		return errorStyle.Render(i.text)
	}
	return loadingStyle.Render(i.text)
}
func (i statusItem) Description() string {
	if i.isError {
		return errorStyle.Render(i.desc)
	}
	return i.desc
}
func (i statusItem) FilterValue() string { return "" }

func newList(width, height int, vimMode bool) list.Model {
	l := list.New(nil, newListDelegate(), width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	if vimMode {
		l.KeyMap.PrevPage.SetKeys("left", "pgup", "b", "u")
		l.KeyMap.NextPage.SetKeys("right", "pgdown", "f")
	}
	return l
}

func isPlayableURI(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:")
}

func isEpisodeURI(uri string) bool {
	return strings.HasPrefix(uri, "spotify:episode:")
}

func idFromURI(uri string) string {
	if i := strings.LastIndex(uri, ":"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
