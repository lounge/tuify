package ui

import (
	"context"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// maxAlbumArtBytes caps image downloads so a hostile or malformed URL can't
// stream unbounded data into image.Decode. Spotify artwork fits comfortably
// under this; anything larger is almost certainly not the expected content.
const maxAlbumArtBytes = 5 * 1024 * 1024

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
