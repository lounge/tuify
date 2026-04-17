package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loadingSpinner is a single spinner instance shared by every "loading" UI
// in the package — list status rows, the device selector, and the
// now-playing "Switching to…" banner. A global is cleaner than giving
// each view its own spinner: the Tick chain is started once from Model.Init
// and a single spinner.TickMsg per frame updates the current frame, which
// every consumer reads via loadingSpinner.View().
var loadingSpinner = newLoadingSpinner()

func newLoadingSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(colorPrimary)
	return s
}

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
	// spinning adds an animated spinner prefix to Title. True for active
	// loading states ("Loading…", "Searching…", "Loading more…"); left
	// false for final-state rows ("No results", "No matching results")
	// and errors so those don't flicker in place.
	spinning bool
}

var loadingStatusItem = statusItem{text: "Loading...", spinning: true}

func (i statusItem) Title() string {
	if i.isError {
		return errorStyle.Render(i.text)
	}
	if i.spinning {
		return loadingSpinner.View() + " " + loadingStyle.Render(i.text)
	}
	return loadingStyle.Render(i.text)
}

// renderStatusLine formats a now-playing status banner: prefixes the
// message with the global spinner frame when spinning, and colors the
// text red for errors. Shared by the full and mini now-playing views so
// both render the same line; without this helper the two call sites
// drift out of sync as styling tweaks land.
func renderStatusLine(msg string, spinning, isError bool) string {
	style := lipgloss.NewStyle().Foreground(colorText)
	if isError {
		style = errorStyle
	}
	rendered := style.Render(msg)
	if spinning {
		rendered = loadingSpinner.View() + " " + rendered
	}
	return rendered
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

func spotifyURL(uri string) string {
	parts := strings.SplitN(uri, ":", 3)
	if len(parts) == 3 {
		return "https://open.spotify.com/" + parts[1] + "/" + parts[2]
	}
	return ""
}

func formatDuration(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
