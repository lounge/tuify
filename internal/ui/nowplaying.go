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

	// flipSettleWindow is how long after Spotify accepted a play/pause or
	// shuffle command a poll that contradicts the optimistic flip is still
	// taken for the API lagging behind. Past it the poll is the state:
	// another client changed it, or the device ignored the command.
	flipSettleWindow = 3 * time.Second

	// nowPlayingPadding is the total horizontal padding (left + right) used
	// in the now-playing area. Kept in sync with Padding(0, 1) in renderGradient.
	nowPlayingPadding = 2
)

// Messages

type playerStateMsg struct {
	seq     uint64 // pollState's sequence number; see appliedPollSeq
	state   *spotify.PlayerState
	err     error
	skipped bool // true when the poll short-circuited (e.g. rate-limit cooldown)
}

type (
	nowPlayingTickMsg time.Time
	progressTickMsg   time.Time
	labelScrollMsg    time.Time
	clearStatusMsg    struct{ seq uint64 }
	delayedPollMsg    struct{}
	episodeResumeMsg  struct{ posMs int }
)

// labelScrollInterval sets the marquee tick rate. 200ms gives a readable
// left-to-right drift without redraw churn.
const labelScrollInterval = 200 * time.Millisecond

// Shuffle indicators. Both are one cell wide so the track line's width
// budget holds whichever is chosen.
const (
	shuffleIconNerdFont = "\U000F049D" // nf-md-shuffle_variant
	shuffleIconFallback = "⇄"
)

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

	// nerdFont selects Nerd Font glyphs (WithNerdFont). The zero value
	// uses the plain-Unicode fallbacks, which render in any font.
	nerdFont bool

	// Playback state
	playing       bool
	shuffling     bool
	hasTrack      bool
	progressMs    int
	durationMs    int
	deviceName    string
	volumePercent int // active device volume 0–100; 100 when no data

	// Pending optimistic updates awaiting API confirmation. Play/pause and
	// shuffle flip ahead of the reply; each flip takes a number from
	// flipSeq, the reply carries it back (playbackResultMsg.flip) and a
	// failure reverts the flip only while that number is still the pending
	// one. So two quick presses, the first of which fails, leave the second
	// flip in place. See pendingFlip for how a success settles it.
	seekPending   bool
	playPauseFlip pendingFlip
	shuffleFlip   pendingFlip
	flipSeq       uint64

	// Polling
	lastUserAction time.Time // zero value means no action yet; pollInterval treats this as idle
	pollSeq        uint64    // numbers each pollState request
	appliedPollSeq uint64    // newest request whose reply has been applied

	// Episode progress resume
	progressCache map[string]int // trackURI → last known progressMs
	resumeUntilMs int            // ignore API progressMs below this until Spotify catches up

	// Device override: when the user manually switches playback to another
	// device in Spotify, we stop re-claiming the preferred device.
	deviceOverridden bool

	// Marquee scroll offset for the "track — artist" label when it doesn't
	// fit in the available width. Measured in display cells and advances
	// once per labelScrollInterval. Resets to 0 on every track change.
	labelScrollOffset int
	// labelTicking is true while a labelScrollMsg is in flight. The shell
	// only keeps the chain alive while the label overflows; see
	// app_tickers.go.
	labelTicking bool

	// Status display (errors and info messages)
	statusMsg     string
	statusIsError bool

	// statusSpinning flips on when statusMsg represents an operation in
	// progress (e.g. transferring playback). The actual spinner frame is
	// rendered from the package-level loadingSpinner, which ticks once for
	// the whole UI — no per-model spinner state or tick chain required.
	statusSpinning bool
	// statusSeq numbers each status message; see setStatus.
	statusSeq uint64
}

// advanceLabelScroll moves the marquee one cell and wraps within the
// composed stream width so the offset stays bounded. Keeps the offset in
// [0, streamW) forever — no int overflow concern on long sessions and no
// reliance on modulo normalization downstream.
func (m *nowPlayingModel) advanceLabelScroll() {
	if w := m.labelStreamWidth(); w > 0 {
		m.labelScrollOffset = (m.labelScrollOffset + 1) % w
	} else {
		m.labelScrollOffset = 0
	}
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

// beginFlip numbers an optimistic flip; see pendingFlip.
func (m *nowPlayingModel) beginFlip() uint64 {
	m.flipSeq++
	return m.flipSeq
}

// newNowPlaying creates a fresh nowPlayingModel. ctx bounds its polls.
func newNowPlaying(ctx context.Context, client *spotify.Client) *nowPlayingModel {
	return &nowPlayingModel{
		ctx:           ctx,
		client:        client,
		progressCache: make(map[string]int),
		volumePercent: 100,
	}
}

// trackInfo describes the playing item for the visualizers.
func (m *nowPlayingModel) trackInfo() trackInfo {
	return trackInfo{
		id:         idFromURI(m.trackURI),
		durationMs: m.durationMs,
		imageURL:   m.imageURL,
		track:      m.track,
		artist:     m.artist,
		isEpisode:  isEpisodeURI(m.trackURI),
	}
}

// Lifecycle

func (m *nowPlayingModel) Init() tea.Cmd {
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
		if msg.seq == m.statusSeq {
			m.statusMsg = ""
			m.statusSpinning = false
		}
		return nil
	}
	return nil
}

func (m *nowPlayingModel) handlePlayerState(msg playerStateMsg) tea.Cmd {
	if msg.seq < m.appliedPollSeq {
		return nil // an older poll finishing after a newer one
	}
	m.appliedPollSeq = msg.seq
	if msg.skipped {
		return nil
	}
	if msg.err != nil {
		log.Printf("[poll] GetPlayerState error: %v", msg.err)
		return nil
	}
	if msg.state == nil {
		// Nothing is playing anywhere (HTTP 204). Playback state is
		// authoritative empty: the flags must not keep the last reported
		// values, or space would send Pause for a stopped player and a
		// later resume would not count as an external change. There is no
		// device to override with either, so the override clears and the
		// next command targets the preferred device; a later poll naming
		// another device re-arms it (deviceName is empty).
		m.hasTrack = false
		m.playing = false
		m.deviceName = ""
		m.playPauseFlip = pendingFlip{}
		m.shuffleFlip = pendingFlip{}
		m.setDeviceOverride(false, "nothing playing anywhere")
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
	// Taken as reported, empty included: an item playing without a
	// context (a queue from search) must not keep the previous one, or
	// withDevice would re-establish playback inside it and syncTo would
	// page a list for an item that isn't playing from it.
	m.contextURI = msg.state.ContextURI
	m.imageURL = msg.state.ImageURL
	m.durationMs = msg.state.DurationMs

	// Detect external device switches: if playback moved away from the
	// preferred device without tuify initiating it, stop re-claiming.
	if preferred := m.client.PreferredDevice(); preferred != "" && msg.state.DeviceName != "" {
		if msg.state.DeviceName != preferred && (m.deviceName == preferred || m.deviceName == "") {
			m.setDeviceOverride(true, fmt.Sprintf("external switch: %s → %s", preferred, msg.state.DeviceName))
		} else if msg.state.DeviceName == preferred && m.deviceOverridden {
			m.setDeviceOverride(false, fmt.Sprintf("playback returned to %s", preferred))
		}
	}
	m.deviceName = msg.state.DeviceName
	m.volumePercent = msg.state.VolumePercent
	m.hasTrack = true

	// Track changed — pending play/pause is stale, accept fresh state.
	if msg.state.TrackURI != prevURI {
		m.playPauseFlip = pendingFlip{}
	}
	// A settled flip the poll still contradicts was overridden; accept it.
	now := time.Now()
	if msg.state.Playing != m.playing && m.playPauseFlip.settled(now) {
		m.playPauseFlip = pendingFlip{}
	}
	if msg.state.Shuffling != m.shuffling && m.shuffleFlip.settled(now) {
		m.shuffleFlip = pendingFlip{}
	}
	if m.playPauseFlip.pending() {
		if msg.state.Playing == m.playing {
			m.playPauseFlip = pendingFlip{}
			m.progressMs = msg.state.ProgressMs
		}
	} else {
		m.playing = msg.state.Playing
		if m.playing {
			// Playback seen running is the user's intent whoever started
			// it. A paused report is not: a dropped librespot session
			// reports exactly that, and the reconnect must still resume.
			m.client.SetPlayIntent(true)
		}
		if !m.seekPending {
			// Until the API catches up to a cached resume position, keep
			// showing that position instead of the lower reported one.
			if m.resumeUntilMs == 0 || msg.state.ProgressMs >= m.resumeUntilMs {
				m.resumeUntilMs = 0
				m.progressMs = msg.state.ProgressMs
			}
		}
	}
	if m.shuffleFlip.pending() {
		if msg.state.Shuffling == m.shuffling {
			m.shuffleFlip = pendingFlip{}
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
	externalChange := (!m.playPauseFlip.pending() && m.playing != prevPlaying) ||
		(prevURI != "" && m.trackURI != prevURI) ||
		(!m.shuffleFlip.pending() && m.shuffling != prevShuffling)
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
	if m.advanceProgress() {
		cmds = append(cmds, m.pollState())
	}
	return tea.Batch(cmds...)
}

// advanceProgress moves the local progress counter one tick forward and
// reports whether this tick just crossed the end of the track — the only
// moment at which the caller should request a fresh player-state poll.
// Once progressMs is clamped at durationMs, subsequent calls return
// false, so a stalled poller (e.g. during a rate-limit cooldown) doesn't
// keep emitting pollState commands every second and racing the regular
// tick's pollState at cooldown expiry.
func (m *nowPlayingModel) advanceProgress() bool {
	if !m.playing || !m.hasTrack {
		return false
	}
	prev := m.progressMs
	m.progressMs += 1000
	if m.progressMs < m.durationMs {
		return false
	}
	m.progressMs = m.durationMs
	return prev < m.durationMs
}

// Status display

func (m *nowPlayingModel) setError(msg string) tea.Cmd {
	return m.setStatus(msg, true, false, 5*time.Second)
}

func (m *nowPlayingModel) setInfo(msg string) tea.Cmd {
	return m.setStatus(msg, false, false, 3*time.Second)
}

// setSpinningInfo shows msg prefixed with the global spinner until the
// status auto-clears (or a subsequent setError / setInfo replaces it).
// Use for operations that take a moment to settle — e.g. "Switching to
// Living Room Speaker" while the device poll confirms the transfer. The
// shell restarts the idle spinner tick on the Update that sets this (see
// app_tickers.go).
func (m *nowPlayingModel) setSpinningInfo(msg string) tea.Cmd {
	return m.setStatus(msg, false, true, 3*time.Second)
}

// setStatus shows msg and schedules its removal after ttl. The clear
// carries a sequence number so a timer left over from an earlier message
// can't wipe a newer one early.
func (m *nowPlayingModel) setStatus(msg string, isError, spinning bool, ttl time.Duration) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = isError
	m.statusSpinning = spinning
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(ttl, func(t time.Time) tea.Msg {
		return clearStatusMsg{seq: seq}
	})
}

// pendingFlip is an optimistic play/pause or shuffle flip waiting for a
// poll to agree. seq is the number beginFlip gave it, 0 when none is
// pending. settleBy is zero while the command is in flight; Spotify
// accepting it sets it flipSettleWindow ahead, and a poll that still
// contradicts the flip after that ends it.
type pendingFlip struct {
	seq      uint64
	settleBy time.Time
}

// pending reports whether a flip is waiting for a poll to agree.
func (f *pendingFlip) pending() bool { return f.seq != 0 }

// is reports whether seq, the flip number a reply carries, is the
// pending flip.
func (f *pendingFlip) is(seq uint64) bool { return seq != 0 && f.seq == seq }

// accept starts the settle window when the reply for flip seq reports
// success, unless a later press has replaced that flip.
func (f *pendingFlip) accept(seq uint64, now time.Time) {
	if f.is(seq) {
		f.settleBy = now.Add(flipSettleWindow)
	}
}

// settled reports whether the pending flip's settle window has passed,
// so a poll that contradicts it is the state.
func (f *pendingFlip) settled(now time.Time) bool {
	return f.pending() && !f.settleBy.IsZero() && now.After(f.settleBy)
}
