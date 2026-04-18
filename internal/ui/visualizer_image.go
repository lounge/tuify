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

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	m.images.cancel = cancel
	url := imageURL
	ch := m.images.ch
	go func() {
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			select {
			case ch <- fetchResult{err: err, url: url}:
			default:
			}
			return
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			select {
			case ch <- fetchResult{err: err, url: url}:
			default:
			}
			return
		}
		defer resp.Body.Close()
		img, _, err := image.Decode(io.LimitReader(resp.Body, maxAlbumArtBytes))
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
