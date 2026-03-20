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
	viewPlaylists
	viewTracks
	viewPodcasts
	viewEpisodes
)

type Model struct {
	viewStack  []viewKind
	home       homeView
	playlists  playlistView
	tracks     trackView
	podcasts   podcastView
	episodes   episodeView
	nowPlaying nowPlayingModel
	client     *spotify.Client
	deviceID   string
	width      int
	height     int
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
		// Search mode: intercept all keys except up/down
		sl := m.searchableList()
		if sl != nil && sl.searching {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				sl.closeSearch()
				return m, nil
			case "enter":
				selected := sl.list.SelectedItem()
				sl.closeSearch()
				return m, m.playCurrentItem(selected)
			case "backspace":
				runes := []rune(sl.searchQuery)
				if len(runes) > 0 {
					sl.searchQuery = string(runes[:len(runes)-1])
					sl.applyFilter()
				}
				return m, nil
			case "/":
				return m, nil
			case "up", "down":
				// Fall through to view update
			default:
				if len(msg.Runes) > 0 {
					sl.searchQuery += string(msg.Runes)
					sl.applyFilter()
				}
				return m, nil
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
			cmd := m.seekRelative(-5000)
			return m, cmd
		case "d":
			cmd := m.seekRelative(5000)
			return m, cmd
		case "esc":
			m.popView()
			return m, nil
		case "enter":
			return m.handleEnter()
		case "/":
			if sl != nil {
				if sl.openSearch() {
					return m, m.fetchSearchableView()
				}
				return m, nil
			}
		}

	case playResultMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m.nowPlaying, cmd = m.nowPlaying.SetError(msg.err.Error())
			return m, cmd
		}
		if msg.deviceID != "" {
			m.deviceID = msg.deviceID
		}
		return m, tea.Batch(
			m.nowPlaying.pollState(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
		)

	case playbackResultMsg:
		if msg.err != nil {
			var cmd tea.Cmd
			m.nowPlaying, cmd = m.nowPlaying.SetError(msg.err.Error())
			return m, cmd
		}
		return m, tea.Batch(
			m.nowPlaying.pollState(),
			tea.Tick(time.Second, func(t time.Time) tea.Msg { return delayedPollMsg{} }),
		)
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
		case viewPlaylists:
			m.playlists = newPlaylistView(m.client, m.width, m.listHeight())
			m.pushView(viewPlaylists)
			return m, m.playlists.Init()
		case viewPodcasts:
			m.podcasts = newPodcastView(m.client, m.width, m.listHeight())
			m.pushView(viewPodcasts)
			return m, m.podcasts.Init()
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

func (m Model) playItem(itemURI, contextURI string) tea.Cmd {
	client := m.client
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx := context.Background()
		if deviceID == "" {
			var err error
			deviceID, err = client.FindDevice(ctx)
			if err != nil {
				return playResultMsg{err: err}
			}
		}
		err := client.Play(ctx, itemURI, contextURI, deviceID)
		return playResultMsg{deviceID: deviceID, err: err}
	}
}

func (m Model) withDevice(fn func(ctx context.Context, client *spotify.Client, deviceID string) error) tea.Cmd {
	client := m.client
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx := context.Background()
		if deviceID == "" {
			var err error
			deviceID, err = client.FindDevice(ctx)
			if err != nil {
				return playbackResultMsg{err: err}
			}
		}
		return playbackResultMsg{err: fn(ctx, client, deviceID)}
	}
}

func (m Model) togglePlayPause() tea.Cmd {
	playing := m.nowPlaying.playing
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		if playing {
			return c.Pause(ctx, id)
		}
		return c.Resume(ctx, id)
	})
}

func (m Model) nextTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Next(ctx, id)
	})
}

func (m Model) previousTrack() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Previous(ctx, id)
	})
}

func (m Model) toggleShuffle() tea.Cmd {
	newState := !m.nowPlaying.shuffling
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Shuffle(ctx, newState, id)
	})
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
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Seek(ctx, posMs, id)
	})
}

func (m Model) stopPlayback() tea.Cmd {
	return m.withDevice(func(ctx context.Context, c *spotify.Client, id string) error {
		return c.Stop(ctx, id)
	})
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
	if sl := m.searchableList(); sl != nil {
		searchEnabled = true
		if sl.searching {
			searchActive = true
			searchQuery = sl.searchQuery
		}
	}
	b.WriteString(m.nowPlaying.View(searchEnabled, searchActive, searchQuery))

	return b.String()
}
