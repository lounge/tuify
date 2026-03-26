package ui

import (
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/lyrics"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// asyncLoader manages a buffered channel for a background fetch with optional
// cancellation. Both the image and lyrics loaders share this lifecycle.
type asyncLoader[R any] struct {
	ch     chan R
	cancel context.CancelFunc
}

func newAsyncLoader[R any]() asyncLoader[R] {
	return asyncLoader[R]{ch: make(chan R, 1)}
}

// drain reads all available results and calls fn for each.
func (l *asyncLoader[R]) drain(fn func(R)) {
	for {
		select {
		case r := <-l.ch:
			fn(r)
		default:
			return
		}
	}
}

// cancelPending cancels any in-flight operation and drains stale results.
func (l *asyncLoader[R]) cancelPending() {
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
}

// boundedCache is a map that evicts all entries except the current key when full.
type boundedCache[K comparable, V any] struct {
	m   map[K]V
	cap int
}

func newBoundedCache[K comparable, V any](cap int) boundedCache[K, V] {
	return boundedCache[K, V]{m: make(map[K]V), cap: cap}
}

func (c *boundedCache[K, V]) get(key K) (V, bool) {
	v, ok := c.m[key]
	return v, ok
}

// put stores val under key. If the cache is full, all entries except keepKey
// are evicted first.
func (c *boundedCache[K, V]) put(key K, val V, keepKey K) {
	if len(c.m) >= c.cap {
		keep, ok := c.m[keepKey]
		c.m = make(map[K]V)
		if ok {
			c.m[keepKey] = keep
		}
	}
	c.m[key] = val
}

type vizTickMsg struct{}

type fetchResult struct {
	img image.Image
	url string
	err error
}

type lyricsFetchResult struct {
	trackID      string
	lines        []string
	instrumental bool
	err          error
}

type cachedLyrics struct {
	lines        []string
	instrumental bool
}

type visualizerModel struct {
	active      bool
	trackID     string
	vizList     []visualizers.Visualizer
	vizIdx      int
	imageURL    string
	images      asyncLoader[fetchResult]
	imageCache  boundedCache[string, image.Image]
	lyrics      asyncLoader[lyricsFetchResult]
	lyricsCache boundedCache[string, cachedLyrics]
	audioRecv   *audio.Receiver
}

func newVisualizerModel(hasAudio bool) *visualizerModel {
	var vizList []visualizers.Visualizer
	if hasAudio {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
			visualizers.NewStarfield(),
			visualizers.NewSpectrum(),
			visualizers.NewOscillogram(),
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
		vizList:     vizList,
		images:      newAsyncLoader[fetchResult](),
		imageCache:  newBoundedCache[string, image.Image](20),
		lyrics:      newAsyncLoader[lyricsFetchResult](),
		lyricsCache: newBoundedCache[string, cachedLyrics](20),
	}
}

func (m *visualizerModel) viz() visualizers.Visualizer {
	return m.vizList[m.vizIdx]
}

func (m *visualizerModel) toggle(trackID string, durationMs int, imageURL, track, artist string) tea.Cmd {
	if m.active {
		m.active = false
		return nil
	}
	m.active = true
	m.drainImages()
	m.drainLyrics()
	if trackID != m.trackID {
		m.trackID = trackID
		for _, v := range m.vizList {
			v.Init(trackID, durationMs)
		}
		m.loadLyrics(trackID, track, artist)
	}
	m.loadImage(imageURL)
	return m.tick()
}

func (m *visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance(progressMs int) {
	m.drainImages()
	m.drainLyrics()
	v := m.viz()
	if m.audioRecv != nil {
		if aa, ok := v.(visualizers.AudioAware); ok {
			aa.SetAudioData(m.audioRecv.Latest())
		}
	}
	if pa, ok := v.(visualizers.ProgressAware); ok {
		pa.SetProgress(progressMs)
	}
	v.Advance()
}

func (m *visualizerModel) cycle(delta int) {
	m.vizIdx = (m.vizIdx + delta + len(m.vizList)) % len(m.vizList)
}

func (m *visualizerModel) onTrackChange(trackID string, durationMs int, track, artist string) {
	m.trackID = trackID
	for _, v := range m.vizList {
		v.Init(trackID, durationMs)
	}
	m.loadLyrics(trackID, track, artist)
}

// --- Image loading ---

func (m *visualizerModel) loadImage(imageURL string) {
	if imageURL == "" {
		m.imageURL = ""
		m.setFallbackImage()
		return
	}
	m.imageURL = imageURL
	if img, ok := m.imageCache.get(imageURL); ok {
		m.setImageOnAware(img)
		return
	}
	url := imageURL
	ch := m.images.ch
	go func() {
		resp, err := httpClient.Get(url)
		if err != nil {
			select {
			case ch <- fetchResult{err: err, url: url}:
			default:
			}
			return
		}
		defer resp.Body.Close()
		img, _, err := image.Decode(resp.Body)
		select {
		case ch <- fetchResult{img: img, url: url, err: err}:
		default:
		}
	}()
}

func (m *visualizerModel) drainImages() {
	m.images.drain(func(r fetchResult) {
		if r.err != nil {
			log.Printf("[visualizer] image fetch error for %s: %v", r.url, r.err)
			return
		}
		if r.img == nil {
			return
		}
		m.imageCache.put(r.url, r.img, m.imageURL)
		if r.url == m.imageURL {
			m.setImageOnAware(r.img)
		}
	})
}

func (m *visualizerModel) setImageOnAware(img image.Image) {
	for _, v := range m.vizList {
		if ia, ok := v.(visualizers.ImageAware); ok {
			ia.SetImage(img)
		}
	}
}

func (m *visualizerModel) setFallbackImage() {
	m.setImageOnAware(visualizers.MusicNoteFallback())
}

// --- Lyrics loading ---

func (m *visualizerModel) loadLyrics(trackID, track, artist string) {
	m.lyrics.cancelPending()
	m.drainLyrics()

	if cached, ok := m.lyricsCache.get(trackID); ok {
		if cached.instrumental {
			m.setInstrumentalOnAware()
		} else {
			m.setLyricsOnAware(cached.lines)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	m.lyrics.cancel = cancel
	ch := m.lyrics.ch
	go func() {
		defer cancel()
		text, err := lyrics.Search(ctx, httpClient, track, artist)
		res := lyricsFetchResult{trackID: trackID, err: err}
		if errors.Is(err, lyrics.ErrInstrumental) {
			res.instrumental = true
			res.err = nil
		} else if err == nil && text != "" {
			res.lines = strings.Split(text, "\n")
		}
		select {
		case ch <- res:
		default:
			log.Printf("[visualizer] lyrics result dropped for %s (channel full)", trackID)
		}
	}()
}

func (m *visualizerModel) drainLyrics() {
	m.lyrics.drain(func(r lyricsFetchResult) {
		if r.err != nil {
			log.Printf("[visualizer] lyrics fetch error for %s: %v", r.trackID, r.err)
			if r.trackID == m.trackID {
				m.setLyricsOnAware(nil)
			}
			return
		}
		m.lyricsCache.put(r.trackID, cachedLyrics{
			lines:        r.lines,
			instrumental: r.instrumental,
		}, m.trackID)
		if r.trackID == m.trackID {
			if r.instrumental {
				m.setInstrumentalOnAware()
			} else {
				m.setLyricsOnAware(r.lines)
			}
		}
	})
}

func (m *visualizerModel) setLyricsOnAware(lines []string) {
	for _, v := range m.vizList {
		if la, ok := v.(visualizers.LyricsAware); ok {
			la.SetLyrics(lines)
		}
	}
}

func (m *visualizerModel) setInstrumentalOnAware() {
	for _, v := range m.vizList {
		if la, ok := v.(visualizers.LyricsAware); ok {
			la.SetInstrumental()
		}
	}
}

func (m *visualizerModel) View(width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	return m.viz().View(width, height)
}
