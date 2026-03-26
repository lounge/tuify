package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

type playerStateMsg struct {
	state *spotify.PlayerState
	err   error
}

type nowPlayingTickMsg time.Time
type progressTickMsg time.Time
type clearErrorMsg struct{}
type delayedPollMsg struct{}

type nowPlayingModel struct {
	client           *spotify.Client
	track            string
	artist           string
	trackURI         string
	imageURL         string
	playing          bool
	shuffling        bool
	hasTrack         bool
	errMsg           string
	width            int
	progressMs       int
	durationMs       int
	seekPending      bool
	playPausePending bool
	shufflePending   bool
	lastUserAction   time.Time // zero value means no action yet; pollInterval treats this as idle
}

func (m *nowPlayingModel) recordUserAction() {
	m.lastUserAction = time.Now()
}

func newNowPlaying(client *spotify.Client) *nowPlayingModel {
	return &nowPlayingModel{client: client}
}

func (m nowPlayingModel) Init() tea.Cmd {
	return tea.Batch(m.pollState(), m.tick(), m.progressTick())
}

func (m nowPlayingModel) tick() tea.Cmd {
	interval := m.pollInterval()
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return nowPlayingTickMsg(t)
	})
}

func (m nowPlayingModel) pollInterval() time.Duration {
	if !m.hasTrack {
		return 10 * time.Second
	}
	if time.Since(m.lastUserAction) < 30*time.Second {
		return 5 * time.Second
	}
	if !m.playing {
		return 15 * time.Second
	}
	if m.durationMs-m.progressMs < 15000 {
		return 3 * time.Second
	}
	return 10 * time.Second
}

func (m nowPlayingModel) progressTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
	})
}

func (m nowPlayingModel) pollState() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		state, err := client.GetPlayerState(context.Background())
		return playerStateMsg{state: state, err: err}
	}
}

func (m *nowPlayingModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case playerStateMsg:
		if msg.err != nil {
			log.Printf("[poll] GetPlayerState error: %v", msg.err)
			return nil
		}
		if msg.state != nil {
			prevURI := m.trackURI
			prevPlaying := m.playing
			prevShuffling := m.shuffling

			m.track = msg.state.TrackName
			m.artist = msg.state.ArtistName
			m.trackURI = msg.state.TrackURI
			m.imageURL = msg.state.ImageURL
			// Track changed — pending play/pause is stale, accept fresh state
			if m.playPausePending && msg.state.TrackURI != prevURI {
				m.playPausePending = false
			}
			if m.playPausePending {
				if msg.state.Playing == m.playing {
					m.playPausePending = false
					m.progressMs = msg.state.ProgressMs
				}
			} else {
				m.playing = msg.state.Playing
				if !m.seekPending {
					m.progressMs = msg.state.ProgressMs
				}
			}
			if m.shufflePending {
				if msg.state.Shuffling == m.shuffling {
					m.shufflePending = false
				}
			} else {
				m.shuffling = msg.state.Shuffling
			}
			m.durationMs = msg.state.DurationMs
			m.hasTrack = true

			// Detect external state changes (from Spotify client, not tuify)
			// and boost polling so follow-up changes are caught quickly.
			externalChange := false
			if !m.playPausePending && m.playing != prevPlaying {
				externalChange = true
			}
			if prevURI != "" && m.trackURI != prevURI {
				externalChange = true
			}
			if !m.shufflePending && m.shuffling != prevShuffling {
				externalChange = true
			}
			if externalChange {
				log.Printf("[poll] external change detected, boosting poll rate")
				m.recordUserAction()
			}

			if m.trackURI != prevURI {
				log.Printf("[poll] track changed → %s — %s", m.track, m.artist)
			}
		} else {
			m.hasTrack = false
		}
		return nil

	case nowPlayingTickMsg:
		return tea.Batch(m.pollState(), m.tick())

	case progressTickMsg:
		cmds := []tea.Cmd{m.progressTick()}
		if m.playing && m.hasTrack {
			m.progressMs += 1000
			if m.progressMs >= m.durationMs {
				m.progressMs = m.durationMs
				cmds = append(cmds, m.pollState())
			}
		}
		return tea.Batch(cmds...)

	case delayedPollMsg:
		return m.pollState()

	case clearErrorMsg:
		m.errMsg = ""
		return nil
	}
	return nil
}

func (m nowPlayingModel) progressBarView() string {
	return renderProgressBar(m.width, m.progressMs, m.durationMs)
}

func (m *nowPlayingModel) SetError(msg string) tea.Cmd {
	m.errMsg = msg
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

func (m nowPlayingModel) View(searchEnabled, searchActive bool, searchQuery string, vizAvailable, vimMode bool) string {
	if m.errMsg != "" {
		return nowPlayingStyle.Width(m.width).Render(
			fmt.Sprintf("%s\n\n\n\n", errorStyle.Render(m.errMsg)),
		)
	}

	var status string
	if m.hasTrack {
		icon := "⏸"
		if m.playing {
			icon = "▶"
		}
		shuffle := ""
		if m.shuffling {
			shuffle = "[shuffle] "
		}
		status = fmt.Sprintf("%s %s%s — %s",
			nowPlayingIconStyle.Render(icon),
			nowPlayingIconStyle.Render(shuffle),
			nowPlayingTrackStyle.Render(m.track),
			nowPlayingArtistStyle.Render(m.artist),
		)
	} else {
		status = nowPlayingArtistStyle.Render("No track playing")
	}

	var progress string
	if m.hasTrack {
		progress = m.progressBarView()
	}

	var help string
	if searchActive {
		if idx := strings.Index(searchQuery, ":"); idx > 0 {
			pre := searchQuery[:idx+1]
			rest := searchQuery[idx+1:]
			help = searchPrefixStyle.Render("/"+pre) + searchInputStyle.Render(rest+"█")
		} else {
			help = searchInputStyle.Render("/" + searchQuery + "█")
		}
	} else {
		vizHint := ""
		if vizAvailable {
			vizHint = "  v:viz"
		}
		searchHint := ""
		if searchEnabled {
			searchHint = "  /:search"
		}
		if vimMode {
			help = helpStyle.Render("hjkl:navigate  space:play  n:next  p:prev  ,/.:seek  r:shuffle  s:stop" + searchHint + vizHint + "  q:quit")
		} else {
			help = helpStyle.Render("space:play/pause  n:next  p:prev  a/d:seek  r:shuffle  s:stop" + searchHint + vizHint + "  q:quit")
		}
	}
	return nowPlayingStyle.Width(m.width).Render(
		fmt.Sprintf("%s\n\n%s\n\n%s", status, progress, help),
	)
}
