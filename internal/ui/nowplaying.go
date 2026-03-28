package ui

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lucasb-eyer/go-colorful"
)

const (
	// episodeResumeThresholdMs is the maximum API-reported progress (in ms)
	// below which we restore cached episode progress instead.
	episodeResumeThresholdMs = 5000

	// nearEndThresholdMs is how close to the end of a track (in ms) before
	// the poll rate increases to catch the track change quickly.
	nearEndThresholdMs = 15000

	// nowPlayingPadding is the total horizontal padding (left + right) used
	// in the now-playing area. Kept in sync with Padding(0, 1) in renderGradient.
	nowPlayingPadding = 2
)

// ansiSGR matches any ANSI SGR (Select Graphic Rendition) escape sequence.
var ansiSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Messages

type playerStateMsg struct {
	state *spotify.PlayerState
	err   error
}

type (
	nowPlayingTickMsg time.Time
	progressTickMsg   time.Time
	clearStatusMsg    struct{}
	delayedPollMsg    struct{}
	episodeResumeMsg  struct{ posMs int }
)

// Model

type nowPlayingModel struct {
	client *spotify.Client
	width  int

	// Track metadata
	track      string
	artist     string
	trackURI   string
	contextURI string
	imageURL   string

	// Playback state
	playing    bool
	shuffling  bool
	hasTrack   bool
	progressMs int
	durationMs int

	// Pending optimistic updates awaiting API confirmation
	seekPending      bool
	playPausePending bool
	shufflePending   bool

	// Polling
	lastUserAction time.Time // zero value means no action yet; pollInterval treats this as idle

	// Episode progress resume
	progressCache map[string]int // trackURI → last known progressMs
	resumeUntilMs int            // ignore API progressMs below this until Spotify catches up

	// Status display (errors and info messages)
	statusMsg     string
	statusIsError bool
}

func newNowPlaying(client *spotify.Client) *nowPlayingModel {
	return &nowPlayingModel{client: client, progressCache: make(map[string]int)}
}

// Lifecycle

func (m nowPlayingModel) Init() tea.Cmd {
	return tea.Batch(m.pollState(), m.tick(), m.progressTick())
}

func (m *nowPlayingModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case playerStateMsg:
		return m.handlePlayerState(msg)
	case nowPlayingTickMsg:
		return tea.Batch(m.pollState(), m.tick())
	case progressTickMsg:
		return m.handleProgressTick()
	case delayedPollMsg:
		return m.pollState()
	case clearStatusMsg:
		m.statusMsg = ""
		return nil
	}
	return nil
}

func (m *nowPlayingModel) handlePlayerState(msg playerStateMsg) tea.Cmd {
	if msg.err != nil {
		log.Printf("[poll] GetPlayerState error: %v", msg.err)
		return nil
	}
	if msg.state == nil {
		m.hasTrack = false
		return nil
	}

	prevURI := m.trackURI
	prevPlaying := m.playing
	prevShuffling := m.shuffling

	// Cache episode progress before the URI changes.
	if msg.state.TrackURI != prevURI && prevURI != "" && isEpisodeURI(prevURI) {
		m.progressCache[prevURI] = m.progressMs
	}

	m.track = msg.state.TrackName
	m.artist = msg.state.ArtistName
	m.trackURI = msg.state.TrackURI
	if msg.state.ContextURI != "" {
		m.contextURI = msg.state.ContextURI
	}
	m.imageURL = msg.state.ImageURL
	m.durationMs = msg.state.DurationMs
	m.hasTrack = true

	// Track changed — pending play/pause is stale, accept fresh state.
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
			if m.resumeUntilMs > 0 && msg.state.ProgressMs < m.resumeUntilMs {
				// API hasn't caught up to the cached resume position yet.
			} else {
				m.resumeUntilMs = 0
				m.progressMs = msg.state.ProgressMs
			}
		}
	}
	if m.shufflePending {
		if msg.state.Shuffling == m.shuffling {
			m.shufflePending = false
		}
	} else {
		m.shuffling = msg.state.Shuffling
	}

	// Restore cached episode progress and request a seek to sync Spotify.
	var resumeCmd tea.Cmd
	if m.trackURI != prevURI {
		m.resumeUntilMs = 0
		if cached, ok := m.progressCache[m.trackURI]; ok && m.progressMs < episodeResumeThresholdMs && cached > m.progressMs {
			m.progressMs = cached
			m.resumeUntilMs = cached
			posMs := cached
			resumeCmd = func() tea.Msg { return episodeResumeMsg{posMs: posMs} }
		}
	}

	// Detect external state changes (from Spotify client, not tuify)
	// and boost polling so follow-up changes are caught quickly.
	externalChange := (!m.playPausePending && m.playing != prevPlaying) ||
		(prevURI != "" && m.trackURI != prevURI) ||
		(!m.shufflePending && m.shuffling != prevShuffling)
	if externalChange {
		log.Printf("[poll] external change detected, boosting poll rate")
		m.recordUserAction()
	}
	if m.trackURI != prevURI {
		log.Printf("[poll] track changed → %s — %s", m.track, m.artist)
	}

	return resumeCmd
}

func (m *nowPlayingModel) handleProgressTick() tea.Cmd {
	cmds := []tea.Cmd{m.progressTick()}
	if m.playing && m.hasTrack {
		m.progressMs += 1000
		if m.progressMs >= m.durationMs {
			m.progressMs = m.durationMs
			cmds = append(cmds, m.pollState())
		}
	}
	return tea.Batch(cmds...)
}

// Polling

func (m nowPlayingModel) tick() tea.Cmd {
	return tea.Tick(m.pollInterval(), func(t time.Time) tea.Msg {
		return nowPlayingTickMsg(t)
	})
}

func (m nowPlayingModel) progressTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return progressTickMsg(t)
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
	if m.durationMs-m.progressMs < nearEndThresholdMs {
		return 3 * time.Second
	}
	return 10 * time.Second
}

func (m nowPlayingModel) pollState() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		state, err := client.GetPlayerState(context.Background())
		return playerStateMsg{state: state, err: err}
	}
}

// Helpers

func (m *nowPlayingModel) recordUserAction() {
	m.lastUserAction = time.Now()
}

func (m *nowPlayingModel) SetError(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = true
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m *nowPlayingModel) SetInfo(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = false
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m nowPlayingModel) progressBarView() string {
	return renderProgressBar(m.width, m.progressMs, m.durationMs)
}

// View

func (m nowPlayingModel) View(searchActive bool, searchQuery string) string {
	if m.statusMsg != "" {
		style := lipgloss.NewStyle().Foreground(colorText)
		if m.statusIsError {
			style = errorStyle
		}
		lines := []string{"", style.Render(m.statusMsg), "", "", ""}
		if searchActive {
			lines = append(lines, "")
		}
		return m.renderGradient(lines)
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

	lines := []string{"", status, "", progress, ""}
	if searchActive {
		var search string
		if idx := strings.Index(searchQuery, ":"); idx > 0 {
			pre := searchQuery[:idx+1]
			rest := searchQuery[idx+1:]
			search = searchPrefixStyle.Render("/"+pre) + searchInputStyle.Render(rest+"█")
		} else {
			search = searchInputStyle.Render("/" + searchQuery + "█")
		}
		lines = append(lines, search)
	}
	return m.renderGradient(lines)
}

// renderGradient renders the now-playing area with a purple background that
// fades from top to bottom.
func (m nowPlayingModel) renderGradient(lines []string) string {
	startC, _ := colorful.Hex(resolveHex(colorGradientStart))
	endC, _ := colorful.Hex(resolveHex(colorGradientEnd))

	// Render the entire block through lipgloss for correct width/wrapping,
	// then apply per-line gradient to the visual output.
	content := strings.Join(lines, "\n")
	rendered := lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(content)
	visualLines := strings.Split(rendered, "\n")

	var b strings.Builder
	total := len(visualLines)

	for i, vl := range visualLines {
		var t float64
		if total > 1 {
			t = float64(i) / float64(total-1)
		}
		c := startC.BlendLab(endC, t).Clamped()
		r, g, bl := c.RGB255()
		bgEsc := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, bl)
		vl = ansiSGR.ReplaceAllString(vl, "${0}"+bgEsc)
		b.WriteString(bgEsc + vl + "\x1b[0m")
		if i < total-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
