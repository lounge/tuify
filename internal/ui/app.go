package ui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/spotify"
)

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
}

// ModelOption configures optional Model features.
type ModelOption func(*Model)

// WithAudioReceiver sets the audio receiver for real-time visualizer data
// and enables the audio-reactive visualizers.
func WithAudioReceiver(r *audio.Receiver) ModelOption {
	return func(m *Model) {
		if r != nil {
			m.visualizer = newVisualizerModel(true)
			m.visualizer.audioRecv = r
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
	return tea.Batch(cmds...)
}

func (m Model) waitForLibrespotInactive() tea.Cmd {
	ch := m.librespotInactiveCh
	return func() tea.Msg {
		<-ch
		return LibrespotInactiveMsg{}
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
		if !m.nowPlaying.deviceOverridden {
			m.nowPlaying.deviceOverridden = true
			m.client.DeviceOverridden.Store(true)
			log.Printf("[device] librespot inactive — playback moved away from %s", m.client.PreferredDevice)
		}
		m.nowPlaying.deviceName = ""
		return m, tea.Batch(m.nowPlaying.pollState(), m.waitForLibrespotInactive())
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
			m.nowPlaying.deviceOverridden = true
			m.client.DeviceOverridden.Store(true)
		} else {
			m.nowPlaying.deviceOverridden = false
			m.client.DeviceOverridden.Store(false)
		}
		m.nowPlaying.deviceName = msg.deviceName
		return m, m.nowPlaying.SetInfo("Switching to " + msg.deviceName)
	}

	return m.handleStateUpdate(msg)
}

// Message handlers

func (m Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.nowPlaying.width = msg.Width
	for _, v := range m.viewStack {
		h := m.height - nowPlayingHeight
		if v.Breadcrumb() != "" {
			h -= breadcrumbHeight
		}
		v.SetSize(msg.Width, h)
	}
	return m, nil
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help overlay: close on h, ? or esc
	if m.showHelp {
		switch msg.String() {
		case "h", "?", "esc":
			m.showHelp = false
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		return m, nil
	}

	// Device selector overlay
	if m.showDeviceSelector {
		switch msg.String() {
		case "esc", "tab":
			m.showDeviceSelector = false
		case "up", "k":
			m.deviceSelector.up()
		case "down", "j":
			m.deviceSelector.down()
		case "enter":
			if dev, ok := m.deviceSelector.selected(); ok {
				m.showDeviceSelector = false
				m.deviceSelector.transferring = true
				m.deviceSelector.transferTarget = dev.Name
				m.deviceSelector.transferDeadline = time.Now().Add(15 * time.Second)
				m.nowPlaying.recordUserAction()
				return m, m.transferDevice(dev)
			}
		case "ctrl+c", "q":
			return m, tea.Quit
		}
		return m, nil
	}

	// Search input: API search with debounce
	if sv, ok := m.currentView().(*searchView); ok && sv.searching {
		sc := searchCtx{
			query: &sv.searchQuery,
			list:  &sv.list,
			close: func() { sv.closeSearch() },
			play: func(item list.Item) tea.Cmd {
				// For container items, drill down instead of playing.
				if !sv.isPlayable() {
					return sv.drillDown(item)
				}
				return sv.playSelected(&m, item)
			},
			onChange: func() tea.Cmd {
				sv.debounceSeq++
				_, term := parseSearch(sv.searchQuery)
				if len([]rune(term)) >= 2 {
					return sv.debounce()
				}
				return nil
			},
		}
		if cmd, handled := handleSearchKey(sc, msg); handled {
			return m, cmd
		}
		return m.handleStateUpdate(msg)
	}

	// Search input: local filter
	sl := m.searchableList()
	if sl != nil && sl.searching {
		sc := searchCtx{
			query: &sl.searchQuery,
			list:  &sl.list,
			close: func() { sl.closeSearch() },
			play: func(item list.Item) tea.Cmd {
				if e, ok := m.currentView().(enterable); ok {
					return e.OnEnter(&m)
				}
				return nil
			},
			onChange: func() tea.Cmd {
				sl.applyFilter()
				return nil
			},
		}
		if cmd, handled := handleSearchKey(sc, msg); handled {
			return m, cmd
		}
		return m.handleStateUpdate(msg)
	}

	// Vim-only bindings (before standard dispatch).
	if m.vimMode {
		switch msg.String() {
		case "h":
			return m.handleBack()
		case "l":
			return m.handleEnter()
		case ",":
			m.nowPlaying.recordUserAction()
			return m, m.seekRelative(-5000)
		case ".":
			m.nowPlaying.recordUserAction()
			return m, m.seekRelative(5000)
		case "ctrl+d":
			return m.halfPage(1)
		case "ctrl+u":
			return m.halfPage(-1)
		}
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case " ":
		m.nowPlaying.recordUserAction()
		wasPlaying := m.nowPlaying.playing
		m.nowPlaying.playing = !wasPlaying
		m.nowPlaying.playPausePending = true
		return m, m.togglePlayPause(wasPlaying)
	case "n":
		m.nowPlaying.recordUserAction()
		return m, m.nextTrack()
	case "p":
		m.nowPlaying.recordUserAction()
		return m, m.previousTrack()
	case "r":
		m.nowPlaying.recordUserAction()
		newShuffle := !m.nowPlaying.shuffling
		m.nowPlaying.shuffling = newShuffle
		m.nowPlaying.shufflePending = true
		return m, m.toggleShuffle(newShuffle)
	case "s":
		m.nowPlaying.recordUserAction()
		return m, m.stopPlayback()
	case "a":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(-5000)
	case "d":
		m.nowPlaying.recordUserAction()
		return m, m.seekRelative(5000)
	case "c":
		return m, m.copyTrackLink()
	case "v":
		if m.miniMode {
			return m, nil
		}
		if m.nowPlaying.hasTrack && isPlayableURI(m.nowPlaying.trackURI) {
			cmd := m.visualizer.toggle(idFromURI(m.nowPlaying.trackURI), m.nowPlaying.durationMs, m.nowPlaying.imageURL, m.nowPlaying.track, m.nowPlaying.artist, isEpisodeURI(m.nowPlaying.trackURI))
			return m, cmd
		}
		return m, nil
	case "left":
		if m.visualizer.active {
			m.visualizer.cycle(-1)
			return m, nil
		}
	case "right":
		if m.visualizer.active {
			m.visualizer.cycle(1)
			return m, nil
		}
	case "tab":
		if m.deviceSelector.transferring {
			return m, nil
		}
		m.deviceSelector.open()
		m.showDeviceSelector = true
		return m, fetchDevicesCmd(m.client)
	case "m":
		m.miniMode = !m.miniMode
		return m, nil
	case "h", "?":
		if !m.miniMode {
			m.showHelp = true
		}
		return m, nil
	case "esc":
		if m.miniMode {
			m.miniMode = false
			return m, nil
		}
		return m.handleBack()
	case "enter":
		if m.miniMode {
			return m, nil
		}
		return m.handleEnter()
	case "/":
		if sv, ok := m.currentView().(*searchView); ok {
			sv.openSearch()
			return m, nil
		}
		if sl != nil {
			if sl.openSearch() {
				return m, m.fetchSearchableView()
			}
			return m, nil
		}
	}

	return m.handleStateUpdate(msg)
}

func (m Model) handlePlaybackResult(msg playbackResultMsg) (tea.Model, tea.Cmd) {
	if msg.seek {
		m.nowPlaying.seekPending = false
	}
	if msg.err != nil {
		if m.nowPlaying.playPausePending {
			m.nowPlaying.playPausePending = false
			m.nowPlaying.playing = !m.nowPlaying.playing
		}
		if m.nowPlaying.shufflePending {
			m.nowPlaying.shufflePending = false
			m.nowPlaying.shuffling = !m.nowPlaying.shuffling
		}
		// Don't show transient network errors in the UI — they recover on their own.
		if errors.Is(msg.err, context.DeadlineExceeded) {
			if msg.seek {
				return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
			}
			return m, nil
		}
		errCmd := m.nowPlaying.SetError(msg.err.Error())
		if msg.seek {
			return m, tea.Batch(
				errCmd,
				tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
			)
		}
		return m, errCmd
	}
	if msg.seek {
		return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
	}
	// Staggered polls to catch the update once the API reflects the change.
	return m, tea.Batch(
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
	)
}

func (m Model) handleVizTick() (tea.Model, tea.Cmd) {
	if m.visualizer.active {
		m.visualizer.advance(m.nowPlaying.progressMs)
		return m, m.visualizer.tick()
	}
	return m, nil
}

func (m Model) handleEpisodeResume(msg episodeResumeMsg) (tea.Model, tea.Cmd) {
	posMs := msg.posMs
	return m, m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	}, true)
}

func (m Model) handleSeekFire(msg seekFireMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seekSeq {
		return m, nil // outdated, a newer seek superseded this one
	}
	posMs := msg.posMs
	return m, m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	}, true)
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

// Navigation

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

// View helpers

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

// Playback commands

func (m Model) playQueue(uris []string) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.PlayQueue(ctx, uris, id)
	}, false)
}

func (m Model) playItem(itemURI, contextURI string) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Play(ctx, itemURI, contextURI, id)
	}, false)
}

func (m Model) togglePlayPause(wasPlaying bool) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if wasPlaying {
			return c.Pause(ctx, id)
		}
		return c.Resume(ctx, id)
	}, false)
}

func (m Model) nextTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Next(ctx, id)
	}, false)
}

func (m Model) previousTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Previous(ctx, id)
	}, false)
}

func (m Model) toggleShuffle(newState bool) tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Shuffle(ctx, newState, id)
	}, false)
}

func (m *Model) seekRelative(deltaMs int) tea.Cmd {
	posMs := m.nowPlaying.progressMs + deltaMs
	if posMs < 0 {
		posMs = 0
	}
	if posMs > m.nowPlaying.durationMs {
		posMs = m.nowPlaying.durationMs
	}
	m.nowPlaying.progressMs = posMs
	m.nowPlaying.seekPending = true
	m.seekSeq++
	seq := m.seekSeq
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return seekFireMsg{seq: seq, posMs: posMs}
	})
}

func (m *Model) copyTrackLink() tea.Cmd {
	if !m.nowPlaying.hasTrack {
		return nil
	}
	url := spotifyURL(m.nowPlaying.trackURI)
	if url == "" {
		return nil
	}
	return func() tea.Msg {
		return clipboardResultMsg{err: clipboard.WriteAll(url)}
	}
}

func (m Model) transferDevice(dev spotify.Device) tea.Cmd {
	return transferDeviceCmd(m.client, dev, m.deviceSelector.activeDeviceID)
}

func (m Model) stopPlayback() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Stop(ctx, id)
	}, false)
}

func (m Model) withDevice(fn func(ctx context.Context, client *spotify.Client, deviceID string) error, seek bool) tea.Cmd {
	client := m.client
	trackURI := m.nowPlaying.trackURI
	contextURI := m.nowPlaying.contextURI
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// If the user manually switched to another device in Spotify,
		// target whatever device is currently active instead of re-claiming.
		if client.DeviceOverridden.Load() {
			log.Printf("[withDevice] DeviceOverridden=true, finding active device")
			deviceID, _, _, err := client.FindDevice(ctx, true)
			if err != nil {
				log.Printf("[withDevice] FindDevice(activeOnly) failed: %v", err)
				return playbackResultMsg{err: err, seek: seek}
			}
			log.Printf("[withDevice] targeting overridden device: %s", deviceID)
			if err := fn(ctx, client, deviceID); err != nil {
				log.Printf("[withDevice] command failed on overridden device: %v", err)
				return playbackResultMsg{err: err, seek: seek}
			}
			return playbackResultMsg{err: nil, seek: seek}
		}

		deviceID, active, preferred, err := client.FindDevice(ctx, false)
		if err != nil {
			log.Printf("[withDevice] FindDevice failed: %v", err)
			return playbackResultMsg{err: err, seek: seek}
		}
		log.Printf("[withDevice] device=%s active=%v preferred=%v overridden=%v", deviceID, active, preferred, client.DeviceOverridden.Load())
		// Re-establish playback only when the preferred device was found but is
		// inactive (e.g. librespot idle). If the preferred device is missing
		// from the API response entirely (flaky API), don't transfer to a
		// fallback device — that would steal playback from the actual player.
		if !active && preferred {
			var transferErr error
			if contextURI != "" && trackURI != "" {
				transferErr = client.Play(ctx, trackURI, contextURI, deviceID)
			} else {
				transferErr = client.TransferPlayback(ctx, deviceID, true)
			}
			if transferErr != nil {
				log.Printf("[playback] device re-establishment failed: %v", transferErr)
			}
		}
		return playbackResultMsg{err: fn(ctx, client, deviceID), seek: seek}
	}
}

// View

func (m Model) helpView(height int) string {
	var lines []string
	if m.vimMode {
		lines = []string{
			"h / l        navigate",
			"ctrl+d / u   half page",
			", / .        seek 5s",
			"space        play / pause",
			"n / p        next / prev",
			"r            shuffle",
			"s            stop",
			"c            copy link",
			"v            visualizer",
			"left / right cycle viz",
			"/            search",
			"tab          devices",
			"m            mini mode",
			"?            close help",
			"q            quit",
		}
	} else {
		lines = []string{
			"enter        select",
			"esc          back",
			"a / d        seek 5s",
			"space        play / pause",
			"n / p        next / prev",
			"r            shuffle",
			"s            stop",
			"c            copy link",
			"v            visualizer",
			"left / right cycle viz",
			"/            search",
			"tab          devices",
			"m            mini mode",
			"h            close help",
			"q            quit",
		}
	}
	box := helpOverlayStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) miniModeView() string {
	np := m.nowPlaying
	if np.statusMsg != "" {
		style := lipgloss.NewStyle().Foreground(colorText)
		if np.statusIsError {
			style = errorStyle
		}
		return np.renderGradient([]string{style.Render(np.statusMsg)})
	}
	if !np.hasTrack {
		return np.renderGradient([]string{nowPlayingArtistStyle.Render("No track playing")})
	}

	icon := "⏸"
	if np.playing {
		icon = "▶"
	}
	iconStr := nowPlayingIconStyle.Render(icon)

	cur := formatDuration(time.Duration(np.progressMs) * time.Millisecond)
	total := formatDuration(time.Duration(np.durationMs) * time.Millisecond)
	timestamps := progressTimeStyle.Render(cur + "/" + total)

	iconLen := lipgloss.Width(iconStr)
	tsLen := lipgloss.Width(timestamps)
	innerWidth := m.width - nowPlayingPadding

	// Track — Artist label, truncated to fit.
	track := np.track
	artist := " — " + np.artist
	labelBudget := innerWidth - iconLen - tsLen - 8 // 4 spaces + 4 min bar
	if labelBudget < 1 {
		labelBudget = 1
	}
	labelRunes := []rune(track + artist)
	if len(labelRunes) > labelBudget {
		truncated := string(labelRunes[:labelBudget-1]) + "…"
		trackRunes := []rune(track)
		if labelBudget-1 <= len(trackRunes) {
			track = string(trackRunes[:labelBudget-1]) + "…"
			artist = ""
		} else {
			artist = string([]rune(truncated)[len(trackRunes):])
		}
	}
	var labelStr string
	if artist != "" {
		labelStr = nowPlayingTrackStyle.Render(track) + nowPlayingArtistStyle.Render(artist)
	} else {
		labelStr = nowPlayingTrackStyle.Render(track)
	}
	labelLen := lipgloss.Width(labelStr)

	// Progress bar fills remaining space.
	barWidth := innerWidth - iconLen - labelLen - tsLen - 4
	var barStr string
	if barWidth >= 4 {
		barStr = renderMiniBar(barWidth, np.progressMs, np.durationMs)
	}

	var line string
	if barStr != "" {
		line = iconStr + " " + labelStr + "  " + barStr + " " + timestamps
	} else {
		line = iconStr + " " + labelStr + "  " + timestamps
	}

	return np.renderGradient([]string{line})
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.showDeviceSelector {
		return m.deviceSelector.view(m.width, m.height)
	}

	if m.miniMode {
		return m.miniModeView()
	}

	var b strings.Builder
	contentHeight := m.height - nowPlayingHeight

	if m.showHelp {
		b.WriteString(m.helpView(contentHeight))
	} else if m.visualizer.active {
		b.WriteString(m.visualizer.View(m.width, contentHeight))
	} else {
		if crumbs := m.currentView().Breadcrumb(); crumbs != "" {
			b.WriteString(breadcrumbStyle.Render(crumbs))
			b.WriteString("\n")
		}
		b.WriteString(m.currentView().View())
	}

	// Now playing bar
	b.WriteString("\n")
	var searchActive bool
	var searchQuery string
	if sv, ok := m.currentView().(*searchView); ok && sv.searching {
		searchActive = true
		searchQuery = sv.searchQuery
	} else if sl := m.searchableList(); sl != nil && sl.searching {
		searchActive = true
		searchQuery = sl.searchQuery
	}
	b.WriteString(m.nowPlaying.View(searchActive, searchQuery))

	return b.String()
}

// handleSearchKey dispatches a key event for a search input session.
// Returns the command and whether the key was handled (false = fall through for up/down).
func handleSearchKey(sc searchCtx, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit, true
	case "esc":
		sc.close()
		return nil, true
	case "enter":
		selected := sc.list.SelectedItem()
		sc.close()
		return sc.play(selected), true
	case "backspace":
		runes := []rune(*sc.query)
		if len(runes) > 0 {
			*sc.query = string(runes[:len(runes)-1])
			return sc.onChange(), true
		}
		return nil, true
	case "/":
		return nil, true
	case "up", "down", "left", "right":
		return nil, false
	default:
		if len(msg.Runes) > 0 {
			*sc.query += string(msg.Runes)
			return sc.onChange(), true
		}
		return nil, true
	}
}
