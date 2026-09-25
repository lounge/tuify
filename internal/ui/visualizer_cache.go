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

// asyncLoader manages the result channel and cancellation for a background
// fetch. Both the image and lyrics loaders share this lifecycle.
//
// Each operation started with begin gets its own 1-slot channel. A cancelled
// predecessor can still finish and send after it was cancelled; with a
// per-operation channel that late result lands in an orphaned channel nobody
// reads, instead of occupying the slot the current operation's result needs.
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

// begin cancels any in-flight operation and starts a new one with a fresh
// result channel. The returned context carries the timeout; the goroutine
// doing the work must call the returned cancel func when it finishes and
// send exactly one result on the returned channel.
//
// Call drain before begin if results already sitting on the previous
// operation's channel should still be processed.
func (l *asyncLoader[R]) begin(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, chan<- R) {
	l.cancelPending()
	ctx, cancel := context.WithTimeout(parent, timeout)
	l.cancel = cancel
	l.ch = make(chan R, 1)
	return ctx, cancel, l.ch
}

// cancelPending cancels any in-flight operation. It does not drain results.
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
