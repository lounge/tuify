package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register the decoders album art arrives in
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
	client := m.httpClient
	go func() {
		defer cancel()
		// Exactly one send on this operation's own 1-slot channel, so it
		// never blocks.
		ch <- fetchImage(ctx, client, url)
	}()
}

func fetchImage(ctx context.Context, client *http.Client, url string) fetchResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fetchResult{err: err, url: url}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fetchResult{err: err, url: url}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetchResult{err: fmt.Errorf("album art: HTTP %d", resp.StatusCode), url: url}
	}
	img, err := decodeAlbumArt(io.LimitReader(resp.Body, maxAlbumArtBytes))
	return fetchResult{img: img, url: url, err: err}
}

// decodeAlbumArt decodes an image after checking the dimensions its header
// declares, so an oversized image is rejected before the decoder allocates
// its pixel buffer.
func decodeAlbumArt(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width > maxAlbumArtSide || cfg.Height > maxAlbumArtSide {
		return nil, fmt.Errorf("album art too large: %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
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
