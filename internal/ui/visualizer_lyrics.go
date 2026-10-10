package ui

import (
	"errors"
	"log"
	"time"

	"github.com/lounge/tuify/internal/lyrics"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

type lyricsFetchResult struct {
	trackID      string
	lines        []visualizers.LyricLine
	instrumental bool
	err          error
}

type cachedLyrics struct {
	lines        []visualizers.LyricLine
	instrumental bool
}

// loadLyrics fetches the track's lyrics, serving them from the cache when
// it can. durationMs is the track length, which LRCLIB uses to pick the
// right edit of a song; 0 means unknown.
func (m *visualizerModel) loadLyrics(trackID, track, artist string, durationMs int) {
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

	ctx, cancel, ch := m.lyrics.begin(m.ctx, 15*time.Second)
	client := m.httpClient
	go func() {
		defer cancel()
		res, err := lyrics.Search(ctx, client, track, artist, durationMs)
		out := lyricsFetchResult{trackID: trackID, err: err}
		switch {
		case errors.Is(err, lyrics.ErrInstrumental):
			out.instrumental = true
			out.err = nil
		case err == nil:
			out.lines = toLyricLines(res.Lines)
		}
		ch <- out // never blocks: this operation's own 1-slot channel
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

func (m *visualizerModel) setLyricsOnAware(lines []visualizers.LyricLine) {
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

// toLyricLines converts fetched lines to the visualizers' type, so the
// visualizers package stays free of the lyrics package. An empty input
// gives nil, which the Lyrics visualizer reads as "no lyrics found".
func toLyricLines(lines []lyrics.Line) []visualizers.LyricLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]visualizers.LyricLine, len(lines))
	for i, l := range lines {
		out[i] = visualizers.LyricLine{StartMs: l.StartMs, Text: l.Text}
	}
	return out
}
