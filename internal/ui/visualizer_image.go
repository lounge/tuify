package ui

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/lounge/tuify/internal/ui/visualizers"
)

type fetchResult struct {
	img image.Image
	url string
	err error
}

func (m *visualizerModel) loadImage(imageURL string) {
	m.images.cancelPending()
	m.drainImages()

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

	ctx, cancel, ch := m.images.begin(m.ctx, 10*time.Second)
	url := imageURL
	go func() {
		defer cancel()
		// Exactly one send on this operation's own 1-slot channel, so it
		// never blocks.
		ch <- fetchImage(ctx, url)
	}()
}

func fetchImage(ctx context.Context, url string) fetchResult {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fetchResult{err: err, url: url}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fetchResult{err: err, url: url}
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxAlbumArtBytes))
	return fetchResult{img: img, url: url, err: err}
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
