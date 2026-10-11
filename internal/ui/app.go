package ui

import (
	"context"
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

// defaultWindowTitle is the terminal title while nothing playable plays.
const defaultWindowTitle = "tuify"

// This file holds the core Model plus Init and the view-stack helpers. Message
// types are in app_messages.go, optional constructors in app_options.go,
// Update and navigation in app_update.go. Key handling, message handlers,
// playback commands, and rendering live in app_keys.go, app_handlers.go,
// app_commands.go, and app_view.go respectively.

const (
	// now-playing: blank + status + blank + progress + blank; the search
	// prompt replaces the last blank while a filter is open, so the bar
	// never grows past this.
	nowPlayingHeight = 5
	// breadcrumb text + margin-bottom: 2 lines
	breadcrumbHeight = 2
)

// Model is the root bubbletea model: the view stack, the long-lived
// submodels (now playing, visualizer, device selector) and the channels
// bootstrap wires in. Build it with NewModel.
type Model struct {
	// rootCtx is the app-level context passed down from bootstrap.Run.
	// All Spotify API calls wrap it with a per-operation timeout rather
	// than using context.Background, so on app shutdown pending requests
	// cancel cleanly instead of lingering past tea.Program exit.
	rootCtx            context.Context
	viewStack          []view
	nowPlaying         *nowPlayingModel
	visualizer         *visualizerModel
	client             *spotify.Client
	width              int
	height             int
	seekSeq            int
	vimMode            bool
	showHelp           bool
	showDeviceSelector bool
	deviceSelector     deviceSelectorModel
	miniMode           bool
	// spinnerTicking is true while a loadingSpinner tick is in flight.
	// The chain stops when nothing on screen spins and resumeTickers
	// restarts it; see app_tickers.go.
	spinnerTicking      bool
	librespotInactiveCh <-chan struct{}
	tokenSaveErrCh      <-chan error
	tokenRevokedCh      <-chan struct{}

	// Click state for double-click detection. When a left click lands on a
	// zoned item, we record its zone id and timestamp; a second click on
	// the same id within doubleClickWindow fires the enter action. The id
	// is a row (list id + index), not a URI, so two rows showing the same
	// track don't pair up.
	lastClickID   string
	lastClickTime time.Time

	// Wheel debounce: OS scroll acceleration emits multiple MouseMsg
	// events per physical wheel notch, so we coalesce events closer
	// together than wheelDebounceWindow into one cursor move.
	lastWheelTime time.Time
}

// NewModel constructs the root UI model. ctx is the app-level lifetime
// plumbed from bootstrap.Run; every Spotify API call spawned by the UI
// wraps it with a per-operation timeout so shutdown cancellation
// cascades instead of leaking pending requests past tea.Program exit.
// Panics on nil ctx — forgetting to pass one would silently downgrade
// shutdown semantics — and on nil client, which would otherwise panic
// later on the first poll or keypress, far from the cause.
func NewModel(ctx context.Context, client *spotify.Client, opts ...ModelOption) Model {
	if ctx == nil {
		panic("ui.NewModel: ctx must not be nil")
	}
	if client == nil {
		panic("ui.NewModel: client must not be nil")
	}
	var o modelOptions
	for _, opt := range opts {
		opt(&o)
	}
	// Submodels get the root ctx at construction so their async ops
	// (poll, image/lyrics fetch) see shutdown cancellation.
	np := newNowPlaying(ctx, client)
	np.nerdFont = o.nerdFont
	return Model{
		rootCtx:             ctx,
		viewStack:           []view{newHomeView(0, 0, o.vimMode)},
		nowPlaying:          np,
		visualizer:          newVisualizerModel(ctx, o.audioSrc),
		client:              client,
		vimMode:             o.vimMode,
		librespotInactiveCh: o.librespotInactiveCh,
		tokenSaveErrCh:      o.tokenSaveErrCh,
		tokenRevokedCh:      o.tokenRevokedCh,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		// The loading spinner and label marquee ticks start on demand
		// from Update (resumeTickers), not here.
		m.nowPlaying.Init(),
	}
	if m.librespotInactiveCh != nil {
		cmds = append(cmds, m.waitForLibrespotInactive())
	}
	if m.tokenSaveErrCh != nil {
		cmds = append(cmds, m.waitForTokenSaveErr())
	}
	if m.tokenRevokedCh != nil {
		cmds = append(cmds, m.waitForTokenRevoked())
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

// contentHeight is the height above the now-playing bar: the screen, the
// visualizer pane or the help overlay.
func (m Model) contentHeight() int {
	return m.height - nowPlayingHeight
}

func (m Model) listHeight() int {
	return m.contentHeight() - breadcrumbHeight
}

func (m Model) currentList() *list.Model {
	if lp, ok := m.currentView().(listProvider); ok {
		return lp.listModel()
	}
	return nil
}

func (m *Model) searchableList() *lazyList {
	if sp, ok := m.currentView().(searchableListProvider); ok {
		return sp.searchableList()
	}
	return nil
}

func (m Model) fetchSearchableView() tea.Cmd {
	if sp, ok := m.currentView().(searchableListProvider); ok {
		return sp.fetchMore()
	}
	return nil
}
