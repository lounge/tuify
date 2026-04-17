package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/spotify"
)

// This file holds the core Model, its Init/Update dispatcher, handleStateUpdate
// (the central pass that updates now-playing + the current view), navigation
// actions (back/enter/halfPage), and the small view-stack helpers. Key
// handling, message handlers, playback commands, and rendering live in
// app_keys.go, app_handlers.go, app_commands.go, and app_view.go respectively.

const (
	// now-playing: blank + status + blank + progress + blank (+ search when active)
	nowPlayingHeight = 5
	// breadcrumb text + margin-bottom: 2 lines
	breadcrumbHeight = 2
)

// Messages

type seekFireMsg struct {
	seq   int
	posMs int
}

type clipboardResultMsg struct{ err error }

// playbackResultMsg is used for all device-bound commands.
type playbackResultMsg struct {
	err  error
	seek bool // true for seek results (uses lighter post-action polling)
}

// LibrespotInactiveMsg is sent (via p.Send) when librespot reports that the
// device became inactive, indicating playback moved to another device.
type LibrespotInactiveMsg struct{}

// TokenSaveErrMsg is delivered when the auth layer fails to persist a
// refreshed OAuth token. The UI surfaces this as a visible warning because
// the in-memory token still works for the session — but the user will be
// forced to log in again on next restart, and without a signal they have
// no way to connect that to a fixable cause (permissions, disk full, etc.).
type TokenSaveErrMsg struct{ Err error }

// searchCtx captures the parts that differ between API search and local filter search.
type searchCtx struct {
	query    *string
	list     *list.Model
	close    func()
	play     func(list.Item) tea.Cmd
	onChange func() tea.Cmd
}

// Model

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
}

// ModelOption configures optional Model features.
type ModelOption func(*Model)

// AudioSource provides real-time FFT data for the visualizer.
// Implemented by audio.PipeReader.
type AudioSource interface {
	Latest() *audio.FrequencyData
}

// WithAudioSource sets the audio source for real-time visualizer data
// and enables the audio-reactive visualizers.
func WithAudioSource(src AudioSource) ModelOption {
	return func(m *Model) {
		if src != nil {
			m.visualizer = newVisualizerModel(true)
			m.visualizer.audioSrc = src
		}
	}
}

// WithVimMode enables vim-style keybindings (h/l for back/select, ctrl+d/u half-page, etc.).
func WithVimMode() ModelOption {
	return func(m *Model) { m.vimMode = true }
}

// WithLibrespotInactive provides a channel that signals when librespot reports
// its device became inactive (playback moved to another device).
func WithLibrespotInactive(ch <-chan struct{}) ModelOption {
	return func(m *Model) { m.librespotInactiveCh = ch }
}

// WithTokenSaveErrors provides a channel that emits OAuth token persistence
// failures. Each value is rendered as a visible warning so the user can tell
// why they're getting logged out between sessions.
func WithTokenSaveErrors(ch <-chan error) ModelOption {
	return func(m *Model) { m.tokenSaveErrCh = ch }
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

// Lifecycle

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.nowPlaying.Init()}
	if m.librespotInactiveCh != nil {
		cmds = append(cmds, m.waitForLibrespotInactive())
	}
	if m.tokenSaveErrCh != nil {
		cmds = append(cmds, m.waitForTokenSaveErr())
	}
	return tea.Batch(cmds...)
}

func (m Model) waitForLibrespotInactive() tea.Cmd {
	ch := m.librespotInactiveCh
	return func() tea.Msg {
		<-ch
		return LibrespotInactiveMsg{}
	}
}

func (m Model) waitForTokenSaveErr() tea.Cmd {
	ch := m.tokenSaveErrCh
	return func() tea.Msg {
		err, ok := <-ch
		if !ok {
			return nil
		}
		return TokenSaveErrMsg{Err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case playbackResultMsg:
		return m.handlePlaybackResult(msg)
	case vizTickMsg:
		return m.handleVizTick()
	case episodeResumeMsg:
		return m.handleEpisodeResume(msg)
	case clipboardResultMsg:
		if msg.err != nil {
			return m, m.nowPlaying.SetError("Failed to copy: " + msg.err.Error())
		}
		return m, m.nowPlaying.SetInfo("Copied link to clipboard")
	case seekFireMsg:
		return m.handleSeekFire(msg)
	case LibrespotInactiveMsg:
		m.nowPlaying.setDeviceOverride(true, "librespot inactive — playback moved away from "+m.client.PreferredDevice)
		m.nowPlaying.deviceName = ""
		return m, tea.Batch(m.nowPlaying.pollState(), m.waitForLibrespotInactive())
	case TokenSaveErrMsg:
		return m, tea.Batch(
			m.nowPlaying.SetError("Token save failed: "+msg.Err.Error()),
			m.waitForTokenSaveErr(),
		)
	case devicesLoadedMsg:
		m.deviceSelector.handleLoaded(msg)
		return m, nil
	case transferDeviceMsg:
		if msg.err != nil {
			m.deviceSelector.transferring = false
			if errors.Is(msg.err, context.DeadlineExceeded) {
				return m, nil
			}
			return m, m.nowPlaying.SetError("Transfer failed: " + msg.err.Error())
		}
		// Update override state based on whether the chosen device is preferred.
		if m.client.PreferredDevice != "" && msg.deviceName != m.client.PreferredDevice {
			m.nowPlaying.setDeviceOverride(true, "transferred to non-preferred device "+msg.deviceName)
		} else {
			m.nowPlaying.setDeviceOverride(false, "transferred to preferred device "+msg.deviceName)
		}
		m.nowPlaying.deviceName = msg.deviceName
		return m, m.nowPlaying.SetInfo("Switching to " + msg.deviceName)
	}

	return m.handleStateUpdate(msg)
}

// handleStateUpdate processes now-playing, visualizer, and view updates.
// Called for messages not fully consumed by other handlers.
func (m Model) handleStateUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Update now-playing
	prevURI := m.nowPlaying.trackURI
	cmd := m.nowPlaying.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Clear transfer lock once the poller confirms the target device is active,
	// or if the deadline has passed.
	if m.deviceSelector.transferring {
		if m.nowPlaying.deviceName == m.deviceSelector.transferTarget ||
			time.Now().After(m.deviceSelector.transferDeadline) {
			m.deviceSelector.transferring = false
		}
	}

	// Re-init visualizer on track change and reload album art + lyrics
	if m.nowPlaying.trackURI != prevURI && isPlayableURI(m.nowPlaying.trackURI) {
		m.visualizer.onTrackChange(idFromURI(m.nowPlaying.trackURI), m.nowPlaying.durationMs, m.nowPlaying.track, m.nowPlaying.artist, isEpisodeURI(m.nowPlaying.trackURI))
		m.visualizer.loadImage(m.nowPlaying.imageURL)
		cmds = append(cmds, tea.SetWindowTitle(fmt.Sprintf("tuify — %s — %s", m.nowPlaying.track, m.nowPlaying.artist)))
	} else if m.nowPlaying.imageURL != m.visualizer.imageURL {
		m.visualizer.loadImage(m.nowPlaying.imageURL)
	}

	// Sync list selection when the playing item changes
	if m.nowPlaying.trackURI != prevURI {
		if sv, ok := m.currentView().(syncableView); ok {
			if cmd := sv.SyncURI(m.nowPlaying.trackURI); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// Update current view
	if cmd := m.currentView().Update(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// Navigation actions

func (m Model) handleBack() (tea.Model, tea.Cmd) {
	if m.visualizer.active {
		m.visualizer.active = false
		return m, nil
	}
	if sv, ok := m.currentView().(*searchView); ok && sv.depth > 0 {
		if sv.goBack() {
			return m, sv.goBackFetchCmd()
		}
	}
	m.popView()
	return m, nil
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if e, ok := m.currentView().(enterable); ok {
		return m, e.OnEnter(&m)
	}
	return m, nil
}

func (m Model) halfPage(dir int) (tea.Model, tea.Cmd) {
	l := m.currentList()
	if l == nil {
		return m, nil
	}
	half := m.listHeight() / 4 // list items are ~2 lines tall
	if half < 1 {
		half = 1
	}
	idx := l.Index() + dir*half
	if idx < 0 {
		idx = 0
	}
	if max := len(l.Items()) - 1; idx > max {
		idx = max
	}
	l.Select(idx)
	return m, nil
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
