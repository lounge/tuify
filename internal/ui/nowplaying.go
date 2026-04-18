package ui

import (
	"context"
	"fmt"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
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

// Messages

type playerStateMsg struct {
	state *spotify.PlayerState
	err   error
}

type (
	nowPlayingTickMsg time.Time
	progressTickMsg   time.Time
	labelScrollMsg    time.Time
	clearStatusMsg    struct{}
	delayedPollMsg    struct{}
	episodeResumeMsg  struct{ posMs int }
)

// labelScrollInterval sets the marquee tick rate. 200ms gives a readable
// left-to-right drift without redraw churn.
const labelScrollInterval = 200 * time.Millisecond

// Model

type nowPlayingModel struct {
	client *spotify.Client
	ctx    context.Context // app-level ctx, wrapped with per-op timeout
	width  int

	// Track metadata
	track      string
	artist     string
	trackURI   string
	contextURI string
	imageURL   string

	// Playback state
	playing       bool
	shuffling     bool
	hasTrack      bool
	progressMs    int
	durationMs    int
	deviceName    string
	volumePercent int // active device volume 0–100; 100 when no data

	// Pending optimistic updates awaiting API confirmation
	seekPending      bool
	playPausePending bool
	shufflePending   bool

	// Polling
	lastUserAction time.Time // zero value means no action yet; pollInterval treats this as idle

	// Episode progress resume
	progressCache map[string]int // trackURI → last known progressMs
	resumeUntilMs int            // ignore API progressMs below this until Spotify catches up

	// Device override: when the user manually switches playback to another
	// device in Spotify, we stop re-claiming the preferred device.
	preferredDevice  string
	deviceOverridden bool

	// Marquee scroll offset for the "track — artist" label when it doesn't
	// fit in the available width. Measured in display cells and advances
	// once per labelScrollInterval. Resets to 0 on every track change.
	labelScrollOffset int

	// Status display (errors and info messages)
	statusMsg     string
	statusIsError bool

	// statusSpinning flips on when statusMsg represents an operation in
	// progress (e.g. transferring playback). The actual spinner frame is
	// rendered from the package-level loadingSpinner, which ticks once for
	// the whole UI — no per-model spinner state or tick chain required.
	statusSpinning bool
}

// setDeviceOverride updates the device override state in both the UI model and
// the spotify client (atomic, read by background goroutines). Logs transitions.
func (m *nowPlayingModel) setDeviceOverride(overridden bool, reason string) {
	if m.deviceOverridden == overridden {
		return
	}
	m.deviceOverridden = overridden
	m.client.DeviceOverridden.Store(overridden)
	if overridden {
		log.Printf("[device] override set: %s", reason)
	} else {
		log.Printf("[device] override cleared: %s", reason)
	}
}

// newNowPlaying creates a fresh nowPlayingModel. The ctx field is left
// zero; NewModel sets it from Model.rootCtx after options apply. Anything
// that triggers a ctx-using path (pollState, etc.) must go through
// NewModel — direct construction is reserved for tests that don't
// exercise those paths.
func newNowPlaying(client *spotify.Client) *nowPlayingModel {
	return &nowPlayingModel{
		client:          client,
		preferredDevice: client.PreferredDevice,
		progressCache:   make(map[string]int),
		volumePercent:   100,
	}
}

// Lifecycle

func (m nowPlayingModel) Init() tea.Cmd {
	return tea.Batch(m.pollState(), m.tick(), m.progressTick(), m.labelScrollTick())
}

func (m *nowPlayingModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case playerStateMsg:
		return m.handlePlayerState(msg)
	case nowPlayingTickMsg:
		return tea.Batch(m.pollState(), m.tick())
	case progressTickMsg:
		return m.handleProgressTick()
	case labelScrollMsg:
		// Advance the marquee and wrap within the composed stream width
		// so the offset stays bounded. Keeps the offset in [0, streamW)
		// forever — no int overflow concern on long sessions and no
		// reliance on modulo normalization downstream.
		if w := m.labelStreamWidth(); w > 0 {
			m.labelScrollOffset = (m.labelScrollOffset + 1) % w
		} else {
			m.labelScrollOffset = 0
		}
		return m.labelScrollTick()
	case delayedPollMsg:
		return m.pollState()
	case clearStatusMsg:
		m.statusMsg = ""
		m.statusSpinning = false
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

	// Detect external device switches: if playback moved away from the
	// preferred device without tuify initiating it, stop re-claiming.
	if m.preferredDevice != "" && msg.state.DeviceName != "" {
		if msg.state.DeviceName != m.preferredDevice && (m.deviceName == m.preferredDevice || m.deviceName == "") {
			m.setDeviceOverride(true, fmt.Sprintf("external switch: %s → %s", m.preferredDevice, msg.state.DeviceName))
		} else if msg.state.DeviceName == m.preferredDevice && m.deviceOverridden {
			m.setDeviceOverride(false, fmt.Sprintf("playback returned to %s", m.preferredDevice))
		}
	}
	m.deviceName = msg.state.DeviceName
	m.volumePercent = msg.state.VolumePercent
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
		m.labelScrollOffset = 0 // restart the marquee for the new track
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

// Status display

func (m *nowPlayingModel) SetError(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = true
	m.statusSpinning = false
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m *nowPlayingModel) SetInfo(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = false
	m.statusSpinning = false
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// SetSpinningInfo shows msg prefixed with the global spinner until the
// status auto-clears (or a subsequent SetError / SetInfo replaces it).
// Use for operations that take a moment to settle — e.g. "Switching to
// Living Room Speaker" while the device poll confirms the transfer. The
// spinner tick is driven by Model.Init / Model.Update at the top level.
func (m *nowPlayingModel) SetSpinningInfo(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = false
	m.statusSpinning = true
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}
