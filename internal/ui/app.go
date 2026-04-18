package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// doubleClickWindow is the max interval between two left clicks on the
// same item for the pair to count as a double-click (activate).
const doubleClickWindow = 500 * time.Millisecond

// wheelDebounceWindow swallows consecutive wheel events that arrive faster
// than this. Tuned against macOS/iTerm scroll acceleration, which fires
// 2–3 events per physical wheel tick in burst.
const wheelDebounceWindow = 40 * time.Millisecond

// This file holds the core Model plus Init and the view-stack helpers. Message
// types are in app_messages.go, optional constructors in app_options.go,
// Update and navigation in app_update.go. Key handling, message handlers,
// playback commands, and rendering live in app_keys.go, app_handlers.go,
// app_commands.go, and app_view.go respectively.

const (
	// now-playing: blank + status + blank + progress + blank (+ search when active)
	nowPlayingHeight = 5
	// breadcrumb text + margin-bottom: 2 lines
	breadcrumbHeight = 2
)

type Model struct {
	viewStack           []view
	nowPlaying          *nowPlayingModel
	visualizer          *visualizerModel
	client              *spotify.Client
	width               int
	height              int
	seekSeq             int
	vimMode             bool
	showHelp            bool
	showDeviceSelector  bool
	deviceSelector      deviceSelectorModel
	miniMode            bool
	librespotInactiveCh <-chan struct{}
	tokenSaveErrCh      <-chan error

	// Click state for double-click detection. When a left click lands on a
	// zoned item, we record the item URI and timestamp; a second click on
	// the same URI within doubleClickWindow fires the enter action.
	lastClickURI  string
	lastClickTime time.Time

	// Wheel debounce: OS scroll acceleration emits multiple MouseMsg
	// events per physical wheel notch, so we coalesce events closer
	// together than wheelDebounceWindow into one cursor move.
	lastWheelTime time.Time
}

func NewModel(client *spotify.Client, opts ...ModelOption) Model {
	home := newHomeView(0, 0)
	m := Model{
		viewStack:  []view{home},
		nowPlaying: newNowPlaying(client),
		visualizer: newVisualizerModel(false),
		client:     client,
	}
	for _, opt := range opts {
		opt(&m)
	}
	if m.vimMode {
		home.vimMode = true
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.nowPlaying.Init(),
		// Drive the single global spinner used by list loading rows, the
		// device selector, and the now-playing "Switching to…" banner.
		loadingSpinner.Tick,
	}
	if m.librespotInactiveCh != nil {
		cmds = append(cmds, m.waitForLibrespotInactive())
	}
	if m.tokenSaveErrCh != nil {
		cmds = append(cmds, m.waitForTokenSaveErr())
	}
	return tea.Batch(cmds...)
}

// View-stack helpers

func (m Model) currentView() view {
	return m.viewStack[len(m.viewStack)-1]
}

func (m *Model) pushView(v view) {
	m.viewStack = append(m.viewStack, v)
}

func (m *Model) popView() {
	if len(m.viewStack) > 1 {
		m.viewStack = m.viewStack[:len(m.viewStack)-1]
	}
}

func (m Model) listHeight() int {
	return m.height - nowPlayingHeight - breadcrumbHeight
}

func (m Model) currentList() *list.Model {
	if lp, ok := m.currentView().(listProvider); ok {
		return lp.List()
	}
	return nil
}

func (m *Model) searchableList() *lazyList {
	if sp, ok := m.currentView().(searchableListProvider); ok {
		return sp.SearchableList()
	}
	return nil
}

func (m Model) fetchSearchableView() tea.Cmd {
	if sp, ok := m.currentView().(searchableListProvider); ok {
		return sp.FetchMore()
	}
	return nil
}
