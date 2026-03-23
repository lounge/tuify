package ui

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type vizTickMsg struct{}

type fetchResult struct {
	img image.Image
	url string
	err error
}

type visualizerModel struct {
	active     bool
	trackID    string
	vizList    []visualizers.Visualizer
	vizIdx     int
	imageURL   string
	imageCache map[string]image.Image
	imageCh    chan fetchResult
}

func newVisualizerModel() visualizerModel {
	return visualizerModel{
		vizList: []visualizers.Visualizer{
			visualizers.NewAlbumArt(),
			visualizers.NewStarfield(),
			visualizers.NewOscillogram(),
		},
		imageCache: make(map[string]image.Image),
		imageCh:    make(chan fetchResult, 1),
	}
}

func (m *visualizerModel) viz() visualizers.Visualizer {
	return m.vizList[m.vizIdx]
}

func (m *visualizerModel) toggle(trackID string, durationMs int, imageURL string) tea.Cmd {
	if m.active {
		m.active = false
		return nil
	}
	m.active = true
	m.drainImageCh()
	if trackID != m.trackID {
		m.trackID = trackID
		for _, v := range m.vizList {
			v.Init(trackID, durationMs)
		}
	}
	m.loadImage(imageURL)
	return m.tick()
}

func (m visualizerModel) tick() tea.Cmd {
	return tea.Tick(33*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

func (m *visualizerModel) advance() {
	m.drainImageCh()
	for _, v := range m.vizList {
		v.Advance()
	}
}

func (m *visualizerModel) cycle(delta int) {
	m.vizIdx = (m.vizIdx + delta + len(m.vizList)) % len(m.vizList)
}

func (m *visualizerModel) onTrackChange(trackID string, durationMs int) {
	m.trackID = trackID
	for _, v := range m.vizList {
		v.Init(trackID, durationMs)
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
			if result.err != nil || result.img == nil {
				continue
			}
			if len(m.imageCache) >= 20 {
				m.imageCache = make(map[string]image.Image)
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

func (m visualizerModel) View(progressMs, width, height int) string {
	if m.trackID == "" {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			loadingStyle.Render("No track"))
	}
	vizHeight := height
	if height > 2 {
		vizHeight = height - 1
		viz := m.viz().View(progressMs, width, vizHeight)
		hint := helpStyle.Render("← →")
		hintLine := lipgloss.PlaceHorizontal(width, lipgloss.Center, hint)
		return viz + "\n" + hintLine
	}
	return m.viz().View(progressMs, width, vizHeight)
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
