package lyrics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// ErrInstrumental is returned when the source marks a song as instrumental.
var ErrInstrumental = errors.New("instrumental")

const (
	// maxBodyBytes caps how much of any lyrics response is read. An LRCLIB
	// record or a Genius search result is a few KB and a Genius song page a
	// few hundred KB; anything past the cap is cut off instead of being
	// buffered into memory.
	maxBodyBytes = 4 << 20

	// maxRedirects is how many redirects one request may follow, the same
	// bound net/http applies when no CheckRedirect is set.
	maxRedirects = 10
)

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

// hostClient returns a copy of client that follows a redirect only when
// check accepts the target URL, and at most maxRedirects of them. Without
// it net/http follows up to ten redirects to any host and scheme, which
// would let a response from either service send the request, and the
// parsing of its body, anywhere; the host checks on the first request
// would then only cover the first hop. The client belongs to the caller
// and is shared with other fetches, so it is copied per call rather than
// changed in place.
func hostClient(client *http.Client, check func(*url.URL) error) *http.Client {
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if err := check(req.URL); err != nil {
			return fmt.Errorf("redirect refused: %w", err)
		}
		return nil
	}
	return &c
}

// artistMatches reports whether a hit's artist field names artist: a
// case-insensitive substring match after quote normalization, so "Queen,
// David Bowie" matches "Queen" and "Guns N’ Roses" matches "Guns N'
// Roses". An empty artist matches nothing rather than everything
// (strings.Contains is true for the empty string): a hit chosen by title
// alone is more likely someone else's song than the right lyrics.
func artistMatches(hitArtist, artist string) bool {
	if artist == "" {
		return false
	}
	return strings.Contains(strings.ToLower(normalizeQuotes(hitArtist)), strings.ToLower(normalizeQuotes(artist)))
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
