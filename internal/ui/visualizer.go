package ui

import (
	"context"
	"image"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type vizTickMsg struct{}

type visualizerModel struct {
	ctx         context.Context // app-level ctx; wrapped with per-op timeout in fetch helpers
	active      bool
	trackID     string
	isEpisode   bool
	track       trackInfo // the item the visualizers were last initialized for
	vizList     []visualizers.Visualizer
	vizIdx      int
	width       int // the pane size from the last resize; see sizeShown
	height      int
	imageURL    string
	images      asyncLoader[fetchResult]
	imageCache  boundedCache[string, image.Image]
	lyrics      asyncLoader[lyricsFetchResult]
	lyricsCache boundedCache[string, cachedLyrics]
	audioSrc    AudioSource
	audioSeenAt time.Time // sticky flag for "audio was flowing recently"
	// httpClient fetches album art and lyrics. A field rather than a
	// package var so tests can point it at an httptest server.
	httpClient *http.Client
}

// audioStickyWindow keeps audioFlowing() true for this long after the
// last non-nil Latest(), so cycle decisions stay stable across the
// 150ms FFT-staleness threshold and through brief frame-arrival jitter.
const audioStickyWindow = 3 * time.Second

// newVisualizerModel builds the visualizer submodel. ctx bounds its album
// art and lyrics fetches. src may be nil: without audio only the album art
// and lyrics visualizers are offered.
func newVisualizerModel(ctx context.Context, src AudioSource) *visualizerModel {
	var vizList []visualizers.Visualizer
	if src != nil {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
			visualizers.NewStarfield(),
			visualizers.NewSpectrum(),
			visualizers.NewOscillogram(),
			visualizers.NewVUMeter(),
			visualizers.NewSpectrogram(),
			visualizers.NewMilkdropSpiral(),
			visualizers.NewMilkdropTunnel(),
			visualizers.NewMilkdropKaleidoscope(),
			visualizers.NewMilkdropRipple(),
		}
	} else {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
		}
	}
	return &visualizerModel{
		ctx:         ctx,
		audioSrc:    src,
		vizList:     vizList,
		httpClient:  newFetchClient(),
		images:      newAsyncLoader[fetchResult](),
		imageCache:  newBoundedCache[string, image.Image](20),
		lyrics:      newAsyncLoader[lyricsFetchResult](),
		lyricsCache: newBoundedCache[string, cachedLyrics](20),
	}
}

func (m *visualizerModel) viz() visualizers.Visualizer {
	return m.vizList[m.vizIdx]
}

// trackInfo is what the visualizers need to know about the playing item.
type trackInfo struct {
	id         string // Spotify ID, also the visualizers' seed
	durationMs int
	imageURL   string
	track      string
	artist     string
	isEpisode  bool
}

// toggle opens or closes the pane. Opening it fetches the current
// track's album art and lyrics unless they are cached or already on the
// way; see setTrack.
func (m *visualizerModel) toggle(t trackInfo) tea.Cmd {
	if m.active {
		m.active = false
		return nil
	}
	m.active = true
	m.drainImages()
	m.drainLyrics()
	m.refreshAudioSeen()
	if t.id != m.trackID {
		m.initTrack(t)
	}
	m.imageURL = t.imageURL
	if m.shouldSkip(m.vizIdx) {
		m.cycle(1)
	}
	m.sizeShown()
	m.fetchAssets()
	return m.tick()
}

// setTrack switches the visualizers to a new playing item. Album art and
// lyrics come from third parties (the image CDN, lrclib.net, genius.com),
// so they are fetched only while the pane is open: with it closed the
// track is recorded and the fetches wait for toggle. Any fetch still
// running for the previous track is cancelled either way.
func (m *visualizerModel) setTrack(t trackInfo) {
	m.initTrack(t)
	m.imageURL = t.imageURL
	if m.active {
		m.fetchAssets()
		return
	}
	m.images.cancelPending()
	m.lyrics.cancelPending()
}

// setSize records the pane size. Called from Update on each resize; the
// visualizer on screen is sized at once, any other when it is shown.
func (m *visualizerModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.sizeShown()
}

// sizeShown passes the pane size to the visualizer on screen if it lays
// out state by it (SizeAware), so View finds it sized for the frame it is
// asked to draw. Only that one is sized, and only while the pane is open:
// a Milkdrop preset's buffers run to megabytes on a large terminal, so
// presets nobody is watching are not reallocated on every resize.
func (m *visualizerModel) sizeShown() {
	if !m.active {
		return
	}
	if sa, ok := m.viz().(visualizers.SizeAware); ok {
		sa.SetSize(m.width, m.height)
	}
}

// setImageURL records new art for the current track (Spotify can report
// it a poll after the track) and loads it while the pane is open.
func (m *visualizerModel) setImageURL(url string) {
	m.imageURL = url
	if m.active {
		m.loadImage(url)
	}
}

// clearTrack puts the pane in its "No track" state, for an item that is
// not a track or episode (an ad, a local file): the previous track's art
// and lyrics must not stay up, and its pending fetches are abandoned.
func (m *visualizerModel) clearTrack() {
	m.images.cancelPending()
	m.lyrics.cancelPending()
	m.trackID = ""
	m.track = trackInfo{}
	m.imageURL = ""
}

// fetchAssets loads the current track's album art and lyrics, from the
// caches when it can. Only called while the pane is open.
func (m *visualizerModel) fetchAssets() {
	if m.trackID == "" {
		return
	}
	m.loadImage(m.imageURL)
	if !m.isEpisode {
		m.loadLyrics(m.track.id, m.track.track, m.track.artist, m.track.durationMs)
	}
}

func (m *visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance(progressMs int) {
	m.drainImages()
	m.drainLyrics()
	data := m.refreshAudioSeen()
	v := m.viz()
	if aa, ok := v.(visualizers.AudioAware); ok {
		aa.SetAudioData(data)
	}
	if pa, ok := v.(visualizers.ProgressAware); ok {
		pa.SetProgress(progressMs)
	}
	v.Advance()
}

func (m *visualizerModel) isLyricsViz(idx int) bool {
	_, ok := m.vizList[idx].(*visualizers.Lyrics)
	return ok
}

func (m *visualizerModel) cycle(delta int) {
	n := len(m.vizList)
	if n == 0 {
		return
	}
	defer m.sizeShown() // the one switched to may be sized for another pane
	orig := m.vizIdx
	for range n {
		m.vizIdx = (m.vizIdx + delta + n) % n
		if !m.shouldSkip(m.vizIdx) {
			return
		}
	}
	// AlbumArt is always non-skippable in the current list, so the loop
	// always finds a slot in practice. This restore is defensive against
	// future list changes that could leave the user stranded on the
	// last (skipped) slot the loop visited.
	m.vizIdx = orig
}

// shouldSkip returns true if the visualizer at idx is meaningless in the
// current playback state — episode + lyrics, or audio-reactive while no
// fresh PCM has been seen for audioStickyWindow.
func (m *visualizerModel) shouldSkip(idx int) bool {
	if m.isEpisode && m.isLyricsViz(idx) {
		return true
	}
	if _, audioAware := m.vizList[idx].(visualizers.AudioAware); audioAware {
		if !m.audioFlowing() {
			return true
		}
	}
	return false
}

// audioFlowing reports whether audio was seen flowing within the sticky
// window. Read-only; refreshAudioSeen is the writer.
func (m *visualizerModel) audioFlowing() bool {
	if m.audioSrc == nil || m.audioSeenAt.IsZero() {
		return false
	}
	return time.Since(m.audioSeenAt) < audioStickyWindow
}

// refreshAudioSeen probes the audio source, bumps the sticky timestamp
// if a fresh frame is available, and returns the frame (or nil). Called
// from advance() each tick and from toggle() when the pane opens, so
// shouldSkip's view of the world stays current without polling on the
// cycle hot path.
func (m *visualizerModel) refreshAudioSeen() *audio.FrequencyData {
	if m.audioSrc == nil {
		return nil
	}
	data := m.audioSrc.Latest()
	if data != nil {
		m.audioSeenAt = time.Now()
	}
	return data
}

// initTrack resets every visualizer for a new track. It fetches nothing;
// see setTrack and fetchAssets.
func (m *visualizerModel) initTrack(t trackInfo) {
	m.trackID = t.id
	m.isEpisode = t.isEpisode
	m.track = t
	for _, v := range m.vizList {
		v.Init(t.id, t.durationMs)
	}
	if m.shouldSkip(m.vizIdx) {
		m.cycle(1)
	}
}

func (m *visualizerModel) View(width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	return m.viz().View(width, height)
}
