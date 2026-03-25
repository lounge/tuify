package ui

import (
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

type vizTickMsg struct{}

type fetchResult struct {
	img image.Image
	url string
	err error
}

type lyricsFetchResult struct {
	trackID string
	lines   []string
	err     error
}

type visualizerModel struct {
	active      bool
	trackID     string
	vizList     []visualizers.Visualizer
	vizIdx      int
	imageURL    string
	imageCache  map[string]image.Image
	imageCh     chan fetchResult
	lyricsCh    chan lyricsFetchResult
	lyricsCache map[string][]string
	audioRecv   *audio.Receiver
}

func newVisualizerModel(hasAudio bool) visualizerModel {
	var vizList []visualizers.Visualizer
	if hasAudio {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
			visualizers.NewStarfield(),
			visualizers.NewSpectrum(),
			visualizers.NewOscillogram(),
		}
	} else {
		vizList = []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewLyrics(),
		}
	}
	return visualizerModel{
		vizList:     vizList,
		imageCache:  make(map[string]image.Image),
		imageCh:     make(chan fetchResult, 1),
		lyricsCh:    make(chan lyricsFetchResult, 1),
		lyricsCache: make(map[string][]string),
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
	m.drainImageCh()
	m.drainLyricsCh()
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

func (m visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance(progressMs int) {
	m.drainImageCh()
	m.drainLyricsCh()
	var fd *audio.FrequencyData
	if m.audioRecv != nil {
		fd = m.audioRecv.Latest() // nil when paused or disconnected
	}
	for _, v := range m.vizList {
		if m.audioRecv != nil {
			if aa, ok := v.(visualizers.AudioAware); ok {
				aa.SetAudioData(fd)
			}
		}
		if pa, ok := v.(visualizers.ProgressAware); ok {
			pa.SetProgress(progressMs)
		}
		v.Advance()
	}
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

func (m *visualizerModel) loadLyrics(trackID, track, artist string) {
	if cached, ok := m.lyricsCache[trackID]; ok {
		m.setLyricsOnAware(cached)
		return
	}
	ch := m.lyricsCh
	go func() {
		text, err := lyrics.Search(track, artist)
		var lines []string
		if err == nil && text != "" {
			lines = strings.Split(text, "\n")
		}
		select {
		case ch <- lyricsFetchResult{trackID: trackID, lines: lines, err: err}:
		default:
		}
	}()
}

func (m *visualizerModel) drainLyricsCh() {
	for {
		select {
		case result := <-m.lyricsCh:
			if result.err != nil {
				log.Printf("[visualizer] lyrics fetch error for %s: %v", result.trackID, result.err)
				if result.trackID == m.trackID {
					m.setLyricsOnAware(nil)
				}
				continue
			}
			if len(m.lyricsCache) >= 20 {
				cur := m.lyricsCache[m.trackID]
				m.lyricsCache = make(map[string][]string)
				if cur != nil {
					m.lyricsCache[m.trackID] = cur
				}
			}
			m.lyricsCache[result.trackID] = result.lines
			if result.trackID == m.trackID {
				m.setLyricsOnAware(result.lines)
			}
		default:
			return
		}
	}
}

func (m *visualizerModel) setLyricsOnAware(lines []string) {
	for _, v := range m.vizList {
		if la, ok := v.(visualizers.LyricsAware); ok {
			la.SetLyrics(lines)
		}
	}
}

func (m *visualizerModel) loadImage(imageURL string) {
	if imageURL == "" {
		m.imageURL = ""
		m.setFallbackImage()
		return
	}
	m.imageURL = imageURL
	if img, ok := m.imageCache[imageURL]; ok {
		m.setImageOnAware(img)
		return
	}
	url := imageURL
	ch := m.imageCh
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(url)
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

func (m *visualizerModel) drainImageCh() {
	for {
		select {
		case result := <-m.imageCh:
			if result.err != nil {
				log.Printf("[visualizer] image fetch error for %s: %v", result.url, result.err)
				continue
			}
			if result.img == nil {
				continue
			}
			if len(m.imageCache) >= 20 {
				cur := m.imageCache[m.imageURL]
				m.imageCache = make(map[string]image.Image)
				if cur != nil {
					m.imageCache[m.imageURL] = cur
				}
			}
			m.imageCache[result.url] = result.img
			if result.url == m.imageURL {
				m.setImageOnAware(result.img)
			}
		default:
			return
		}
	}
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

func (m visualizerModel) View(width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	return m.viz().View(width, height)
}

func isPlayableURI(uri string) bool {
	return strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:")
}

func idFromURI(uri string) string {
	if i := strings.LastIndex(uri, ":"); i >= 0 {
		return uri[i+1:]
	}
	return uri
}
