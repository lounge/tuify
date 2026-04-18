package ui

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/lounge/tuify/internal/lyrics"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

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
