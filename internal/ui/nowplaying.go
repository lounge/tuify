package ui

import (
	"context"
	"fmt"
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
	client     *spotify.Client
	track      string
	artist     string
	trackURI   string
	playing    bool
	shuffling  bool
	hasTrack   bool
	errMsg     string
	width      int
	progressMs int
	durationMs int
}

func newNowPlaying(client *spotify.Client) nowPlayingModel {
	return nowPlayingModel{client: client}
}

func (m nowPlayingModel) Init() tea.Cmd {
	return tea.Batch(m.pollState(), m.tick(), m.progressTick())
}

func (m nowPlayingModel) tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return nowPlayingTickMsg(t)
	})
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

func (m nowPlayingModel) Update(msg tea.Msg) (nowPlayingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case playerStateMsg:
		if msg.err != nil {
			return m, nil
		}
		if msg.state != nil {
			m.track = msg.state.TrackName
			m.artist = msg.state.ArtistName
			m.trackURI = msg.state.TrackURI
			m.playing = msg.state.Playing
			m.shuffling = msg.state.Shuffling
			m.progressMs = msg.state.ProgressMs
			m.durationMs = msg.state.DurationMs
			m.hasTrack = true
		} else {
			m.hasTrack = false
		}
		return m, nil

	case nowPlayingTickMsg:
		return m, tea.Batch(m.pollState(), m.tick())

	case progressTickMsg:
		cmds := []tea.Cmd{m.progressTick()}
		if m.playing && m.hasTrack {
			m.progressMs += 1000
			if m.progressMs >= m.durationMs {
				m.progressMs = m.durationMs
				cmds = append(cmds, m.pollState())
			}
		}
		return m, tea.Batch(cmds...)

	case delayedPollMsg:
		return m, m.pollState()

	case clearErrorMsg:
		m.errMsg = ""
		return m, nil
	}
	return m, nil
}

func (m nowPlayingModel) renderProgressBar() string {
	cur := formatMs(m.progressMs)
	total := formatMs(m.durationMs)

	// content width inside nowPlayingStyle (padding 0,1 = 2 chars horizontal)
	contentWidth := m.width - 2
	// bar width = content width minus timestamps and spacing: "0:00 ··· 0:00"
	barWidth := contentWidth - len(cur) - len(total) - 2
	if barWidth < 4 {
		return fmt.Sprintf("%s / %s", progressTimeStyle.Render(cur), progressTimeStyle.Render(total))
	}

	filled := 0
	if m.durationMs > 0 {
		filled = barWidth * m.progressMs / m.durationMs
	}
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := progressFilledStyle.Render(strings.Repeat("━", filled)) +
		progressEmptyStyle.Render(strings.Repeat("─", empty))

	return progressTimeStyle.Render(cur) + " " + bar + " " + progressTimeStyle.Render(total)
}

func formatMs(ms int) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (m nowPlayingModel) SetError(msg string) (nowPlayingModel, tea.Cmd) {
	m.errMsg = msg
	return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

func (m nowPlayingModel) View() string {
	if m.errMsg != "" {
		return nowPlayingStyle.Width(m.width).Render(
			errorStyle.Render(m.errMsg),
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
			icon,
			nowPlayingArtistStyle.Render(shuffle),
			nowPlayingTrackStyle.Render(m.track),
			nowPlayingArtistStyle.Render(m.artist),
		)
	} else {
		status = nowPlayingArtistStyle.Render("No track playing")
	}

	var progress string
	if m.hasTrack {
		progress = m.renderProgressBar()
	}

	help := helpStyle.Render("space:play/pause  n:next  p:prev  a/d:seek  r:shuffle  s:stop  q:quit")
	return nowPlayingStyle.Width(m.width).Render(
		fmt.Sprintf("%s\n%s\n%s", status, progress, help),
	)
}
