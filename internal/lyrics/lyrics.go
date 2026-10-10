package lyrics

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
)

// ErrInstrumental is returned when the source marks a song as instrumental.
var ErrInstrumental = errors.New("instrumental")

// maxBodyBytes caps how much of any lyrics response is read. An LRCLIB
// record or a Genius search result is a few KB and a Genius song page a
// few hundred KB; anything past the cap is cut off instead of being
// buffered into memory.
const maxBodyBytes = 4 << 20

// Line is one lyric line. StartMs is the playback position at which the
// line starts, or -1 when the source carried no timestamps.
type Line struct {
	StartMs int
	Text    string
}

// Result is the lyrics of one track. An empty Lines means no lyrics were
// found. The lines are either all timed or all untimed; Synced tells
// which.
type Result struct {
	Lines []Line
}

// Synced reports whether the lines carry timestamps.
func (r Result) Synced() bool {
	return len(r.Lines) > 0 && r.Lines[0].StartMs >= 0
}

// Search finds lyrics for a track. It asks LRCLIB for an exact match
// first, then LRCLIB's fuzzy search filtered by duration, and falls back
// to Genius when LRCLIB has nothing or fails. durationMs is the track
// length and may be 0 when unknown.
//
// Returns ErrInstrumental when a source marks the song as instrumental,
// and an empty Result with a nil error when nothing was found. An LRCLIB
// failure is logged and treated as a miss; only Genius's error is
// returned. The context can be used to cancel or set a deadline.
func Search(ctx context.Context, client *http.Client, track, artist string, durationMs int) (Result, error) {
	res, err := lrclibGet(ctx, client, track, artist, durationMs)
	if err == nil && len(res.Lines) == 0 {
		res, err = lrclibSearch(ctx, client, track, artist, durationMs)
	}
	switch {
	case errors.Is(err, ErrInstrumental):
		return Result{}, err
	case err != nil:
		if ctx.Err() != nil {
			return Result{}, err
		}
		log.Printf("[lyrics] lrclib failed for %q / %q, trying genius: %v", artist, track, err)
	case len(res.Lines) > 0:
		return res, nil
	default:
		log.Printf("[lyrics] lrclib miss for %q / %q, trying genius", artist, track)
	}

	text, err := searchGenius(ctx, client, track, artist)
	if err != nil {
		return Result{}, err
	}
	return Result{Lines: plainLines(text)}, nil
}

// plainLines turns untimed lyric text, one line per newline, into Lines
// with no timestamps. Empty text gives nil.
func plainLines(text string) []Line {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	lines := make([]Line, len(parts))
	for i, p := range parts {
		lines[i] = Line{StartMs: -1, Text: p}
	}
	return lines
}
