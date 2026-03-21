package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type viewKind int

const (
	viewHome viewKind = iota
	viewSearch
	viewPlaylists
	viewTracks
	viewPodcasts
	viewEpisodes
)

type seekFireMsg struct {
	seq   int
	posMs int
}

type Model struct {
	viewStack  []viewKind
	home       homeView
	search     searchView
	playlists  playlistView
	tracks     trackView
	podcasts   podcastView
	episodes   episodeView
	nowPlaying nowPlayingModel
	client     *spotify.Client
	deviceID   string
	width      int
	height     int
	seekSeq    int
}

func NewModel(client *spotify.Client) Model {
	return Model{
		viewStack:  []viewKind{viewHome},
		home:       newHomeView(0, 0),
		nowPlaying: newNowPlaying(client),
		client:     client,
	}
}

func (m Model) currentView() viewKind {
	return m.viewStack[len(m.viewStack)-1]
}

func (m *Model) pushView(v viewKind) {
	m.viewStack = append(m.viewStack, v)
}

func (m *Model) popView() {
	if len(m.viewStack) > 1 {
		m.viewStack = m.viewStack[:len(m.viewStack)-1]
	}
}

func (m Model) Init() tea.Cmd {
	return m.nowPlaying.Init()
}

const (
	// now-playing: border-top + status + blank + progress + blank + help
	nowPlayingHeight = 6
	// breadcrumb text + margin-bottom: 2 lines + now-playing
	chromeHeight = 2 + nowPlayingHeight
)

func (m Model) listHeight() int {
	return m.height - chromeHeight
}

// searchCtx captures the parts that differ between API search and local filter search.
type searchCtx struct {
	query    *string
	list     *list.Model
	close    func()
	play     func(list.Item) tea.Cmd
	onChange func() tea.Cmd
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.nowPlaying.width = msg.Width
		h := m.listHeight()
		m.home.width = msg.Width
		m.home.height = msg.Height - nowPlayingHeight
		if m.search.client != nil {
			m.search.list.SetSize(msg.Width, h)
		}
		if m.playlists.client != nil {
			m.playlists.list.SetSize(msg.Width, h)
		}
		if m.tracks.client != nil {
			m.tracks.list.SetSize(msg.Width, h)
		}
		if m.podcasts.client != nil {
			m.podcasts.list.SetSize(msg.Width, h)
		}
		if m.episodes.client != nil {
			m.episodes.list.SetSize(msg.Width, h)
		}
		return m, nil

	case tea.KeyMsg:
		// Search input: API search with debounce
		if m.currentView() == viewSearch && m.search.searching {
			sc := searchCtx{
				query: &m.search.searchQuery,
				list:  &m.search.list,
				close: func() { m.search.closeSearch() },
				play: func(item list.Item) tea.Cmd {
					// For container items, drill down instead of playing.
					if !m.search.isPlayable() {
						return m.search.drillDown(item)
					}
					return m.playCurrentItem(item)
				},
				onChange: func() tea.Cmd {
					m.search.debounceSeq++
					_, term := parseSearch(m.search.searchQuery)
					if len([]rune(term)) >= 2 {
						return m.search.debounce()
					}
					return nil
				},
			}
			if cmd, handled := handleSearchKey(sc, msg); handled {
				return m, cmd
			}
			break
		}

		// Search input: local filter
		sl := m.searchableList()
		if sl != nil && sl.searching {
			sc := searchCtx{
				query: &sl.searchQuery,
				list:  &sl.list,
				close: func() { sl.closeSearch() },
				play:  m.playCurrentItem,
				onChange: func() tea.Cmd {
					sl.applyFilter()
					return nil
				},
			}
			if cmd, handled := handleSearchKey(sc, msg); handled {
				return m, cmd
			}
			break
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ":
			return m, m.togglePlayPause()
		case "n":
			return m, m.nextTrack()
		case "p":
			return m, m.previousTrack()
		case "r":
			return m, m.toggleShuffle()
		case "s":
			return m, m.stopPlayback()
		case "a":
			return m, m.seekRelative(-5000)
		case "d":
			return m, m.seekRelative(5000)
		case "esc":
			if m.currentView() == viewSearch && m.search.depth > 0 {
				if m.search.goBack() {
					return m, m.search.goBackFetchCmd()
				}
			}
			m.popView()
			return m, nil
		case "enter":
			return m.handleEnter()
		case "/":
			if m.currentView() == viewSearch {
				m.search.openSearch()
				return m, nil
			}
			if sl != nil {
				if sl.openSearch() {
					return m, m.fetchSearchableView()
				}
				return m, nil
			}
		}

	case playbackResultMsg:
		if msg.seek {
			m.nowPlaying.seekPending = false
		}
		if msg.deviceID != "" {
			m.deviceID = msg.deviceID
		}
		if msg.err != nil {
			var errCmd tea.Cmd
			m.nowPlaying, errCmd = m.nowPlaying.SetError(msg.err.Error())
			if msg.seek {
				return m, tea.Batch(
					errCmd,
					tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
				)
			}
			return m, errCmd
		}
		if msg.seek {
			// Single delayed poll to re-sync, no immediate poll to avoid snapping back
			return m, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return delayedPollMsg{} })
		}
		return m, tea.Batch(
			m.nowPlaying.pollState(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
		)

	case seekFireMsg:
		if msg.seq != m.seekSeq {
			return m, nil // outdated, a newer seek superseded this one
		}
		posMs := msg.posMs
		return m, m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
			return c.Seek(ctx, posMs, id)
		}, true)
	}

	// Update now-playing
	prevURI := m.nowPlaying.trackURI
	var cmd tea.Cmd
	m.nowPlaying, cmd = m.nowPlaying.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Sync list selection when the playing item changes
	if m.nowPlaying.trackURI != prevURI {
		switch m.currentView() {
		case viewSearch:
			if m.search.isPlayable() {
				if m.search.selectByURI(m.nowPlaying.trackURI) {
					cmds = append(cmds, m.search.fetchMore()...)
				}
			}
		case viewTracks:
			if m.tracks.selectByURI(m.nowPlaying.trackURI) {
				cmds = append(cmds, m.tracks.fetchMore())
			}
		case viewEpisodes:
			if m.episodes.selectByURI(m.nowPlaying.trackURI) {
				cmds = append(cmds, m.episodes.fetchMore())
			}
		}
	}

	// Update current view
	switch m.currentView() {
	case viewHome:
		m.home, cmd = m.home.Update(msg)
	case viewSearch:
		m.search, cmd = m.search.Update(msg)
	case viewPlaylists:
		m.playlists, cmd = m.playlists.Update(msg)
	case viewTracks:
		m.tracks, cmd = m.tracks.Update(msg)
	case viewPodcasts:
		m.podcasts, cmd = m.podcasts.Update(msg)
	case viewEpisodes:
		m.episodes, cmd = m.episodes.Update(msg)
	}
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.currentView() {
	case viewHome:
		hi := m.home.selectedItem()
		switch hi.kind {
		case viewSearch:
			m.search = newSearchView(m.client, m.width, m.listHeight())
			m.pushView(viewSearch)
			return m, nil
		case viewPlaylists:
			m.playlists = newPlaylistView(m.client, m.width, m.listHeight())
			m.pushView(viewPlaylists)
			return m, m.playlists.Init()
		case viewPodcasts:
			m.podcasts = newPodcastView(m.client, m.width, m.listHeight())
			m.pushView(viewPodcasts)
			return m, m.podcasts.Init()
		}
	case viewSearch:
		selected := m.search.list.SelectedItem()
		if si, ok := selected.(statusItem); ok && si.isError {
			return m, m.search.retry()
		}
		if m.search.isPlayable() {
			if cmd := m.playCurrentItem(selected); cmd != nil {
				return m, cmd
			}
		} else if cmd := m.search.drillDown(selected); cmd != nil {
			return m, cmd
		}
	case viewPlaylists:
		selected := m.playlists.list.SelectedItem()
		if pi, ok := selected.(playlistItem); ok {
			m.tracks = newTrackView(m.client, pi.id, pi.name, m.width, m.listHeight())
			m.pushView(viewTracks)
			return m, m.tracks.Init()
		}
		if si, ok := selected.(statusItem); ok && si.isError {
			return m, m.playlists.retryLoad()
		}
	case viewTracks:
		selected := m.tracks.list.SelectedItem()
		if cmd := m.playCurrentItem(selected); cmd != nil {
			return m, cmd
		}
		if si, ok := selected.(statusItem); ok && si.isError {
			return m, m.tracks.retryLoad()
		}
	case viewPodcasts:
		selected := m.podcasts.list.SelectedItem()
		if pi, ok := selected.(podcastItem); ok {
			m.episodes = newEpisodeView(m.client, pi.id, pi.name, m.width, m.listHeight())
			m.pushView(viewEpisodes)
			return m, m.episodes.Init()
		}
		if si, ok := selected.(statusItem); ok && si.isError {
			return m, m.podcasts.retryLoad()
		}
	case viewEpisodes:
		selected := m.episodes.list.SelectedItem()
		if cmd := m.playCurrentItem(selected); cmd != nil {
			return m, cmd
		}
		if si, ok := selected.(statusItem); ok && si.isError {
			return m, m.episodes.retryLoad()
		}
	}
	return m, nil
}

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

func (m Model) withDevice(fn func(ctx context.Context, client *spotify.Client, deviceID string) error, seek bool) tea.Cmd {
	client := m.client
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx := context.Background()
		if deviceID == "" {
			var err error
			deviceID, err = client.FindDevice(ctx)
			if err != nil {
				return playbackResultMsg{err: err, seek: seek}
			}
		}
		return playbackResultMsg{deviceID: deviceID, err: fn(ctx, client, deviceID), seek: seek}
	}
}

func (m Model) togglePlayPause() tea.Cmd {
	playing := m.nowPlaying.playing
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if playing {
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

func (m Model) toggleShuffle() tea.Cmd {
	newState := !m.nowPlaying.shuffling
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

func (m Model) stopPlayback() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Stop(ctx, id)
	}, false)
}

func (m *Model) searchableList() *lazyList {
	switch m.currentView() {
	case viewTracks:
		return &m.tracks.lazyList
	case viewEpisodes:
		return &m.episodes.lazyList
	}
	return nil
}

func (m Model) playCurrentItem(item list.Item) tea.Cmd {
	switch m.currentView() {
	case viewSearch:
		ctx := m.search.contextURI()
		if ctx != "" {
			// Album or show context: use Spotify context URI for continuation
			if ti, ok := item.(trackItem); ok {
				return m.playItem(ti.uri, ctx)
			}
			if ei, ok := item.(episodeItem); ok {
				return m.playItem(ei.uri, ctx)
			}
		}
		// No context (direct track/episode search): queue remaining items
		if ti, ok := item.(trackItem); ok {
			return m.playQueue(m.search.queueFrom(ti.uri))
		}
		if ei, ok := item.(episodeItem); ok {
			return m.playQueue(m.search.queueFrom(ei.uri))
		}
	case viewTracks:
		if ti, ok := item.(trackItem); ok {
			return m.playItem(ti.uri, "spotify:playlist:"+m.tracks.playlistID)
		}
	case viewEpisodes:
		if ei, ok := item.(episodeItem); ok {
			return m.playItem(ei.uri, "spotify:show:"+m.episodes.showID)
		}
	}
	return nil
}

func (m Model) fetchSearchableView() tea.Cmd {
	switch m.currentView() {
	case viewTracks:
		return m.tracks.fetchMore()
	case viewEpisodes:
		return m.episodes.fetchMore()
	}
	return nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Breadcrumb (skip on home)
	if m.currentView() != viewHome {
		var crumbs string
		switch m.currentView() {
		case viewSearch:
			crumbs = m.search.Breadcrumb()
		case viewPlaylists:
			crumbs = "Home > Playlists"
		case viewTracks:
			crumbs = fmt.Sprintf("Home > Playlists > %s", m.tracks.playlistName)
		case viewPodcasts:
			crumbs = "Home > Podcasts"
		case viewEpisodes:
			crumbs = fmt.Sprintf("Home > Podcasts > %s", m.episodes.showName)
		}
		b.WriteString(breadcrumbStyle.Render(crumbs))
		b.WriteString("\n")
	}

	// Current view
	switch m.currentView() {
	case viewHome:
		b.WriteString(m.home.View())
	case viewSearch:
		b.WriteString(m.search.View())
	case viewPlaylists:
		b.WriteString(m.playlists.View())
	case viewTracks:
		b.WriteString(m.tracks.View())
	case viewPodcasts:
		b.WriteString(m.podcasts.View())
	case viewEpisodes:
		b.WriteString(m.episodes.View())
	}

	// Now playing bar
	b.WriteString("\n")
	var searchEnabled, searchActive bool
	var searchQuery string
	if m.currentView() == viewSearch {
		searchEnabled = true
		if m.search.searching {
			searchActive = true
			searchQuery = m.search.searchQuery
		}
	} else if sl := m.searchableList(); sl != nil {
		searchEnabled = true
		if sl.searching {
			searchActive = true
			searchQuery = sl.searchQuery
		}
	}
	b.WriteString(m.nowPlaying.View(searchEnabled, searchActive, searchQuery))

	return b.String()
}
