package lyrics

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	lrclibBase = "https://lrclib.net/api"

	// userAgent identifies tuify to LRCLIB, which asks clients to send
	// one. The version lives in main and is not worth threading through.
	userAgent = "tuify (https://github.com/lounge/tuify)"

	// lrclibDurationToleranceMs is how far a search hit's duration may be
	// from the track's and still count as the same recording. LRCLIB's
	// own exact lookup allows about 2 s.
	lrclibDurationToleranceMs = 2000
)

// lrclibRecord is one track as LRCLIB returns it. The lyric fields are
// JSON null for instrumentals, and syncedLyrics is null when only plain
// text was uploaded; both decode to the empty string.
type lrclibRecord struct {
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"` // seconds
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// lrclibGet asks LRCLIB for the exact track: title and artist must match
// and the duration must be within a couple of seconds. A 404 is a plain
// miss and returns an empty Result with a nil error. durationMs of 0 is
// not sent, since the catalogue holds several lengths of most songs and
// a guess would hide the right one.
func lrclibGet(ctx context.Context, client *http.Client, track, artist string, durationMs int) (Result, error) {
	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", track)
	if durationMs > 0 {
		q.Set("duration", strconv.Itoa(roundSeconds(durationMs)))
	}
	resp, err := lrclibDo(ctx, client, lrclibBase+"/get?"+q.Encode())
	if err != nil {
		return Result{}, fmt.Errorf("lrclib get: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Result{}, nil
	default:
		return Result{}, fmt.Errorf("lrclib get: status %d", resp.StatusCode)
	}
	var rec lrclibRecord
	if err := json.UnmarshalRead(io.LimitReader(resp.Body, maxBodyBytes), &rec); err != nil {
		return Result{}, fmt.Errorf("lrclib get: %w", err)
	}
	return recordResult(rec)
}

// lrclibSearch runs LRCLIB's fuzzy search with the cleaned title (remix,
// remaster and feat. suffixes stripped) and picks the hit that is within
// lrclibDurationToleranceMs of the track and names the artist, preferring
// synced lyrics over plain and either over an instrumental marker. No
// such hit returns an empty Result with a nil error.
func lrclibSearch(ctx context.Context, client *http.Client, track, artist string, durationMs int) (Result, error) {
	q := url.Values{}
	q.Set("track_name", improveQuery(track))
	q.Set("artist_name", artist)
	resp, err := lrclibDo(ctx, client, lrclibBase+"/search?"+q.Encode())
	if err != nil {
		return Result{}, fmt.Errorf("lrclib search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("lrclib search: status %d", resp.StatusCode)
	}
	var recs []lrclibRecord
	if err := json.UnmarshalRead(io.LimitReader(resp.Body, maxBodyBytes), &recs); err != nil {
		return Result{}, fmt.Errorf("lrclib search: %w", err)
	}
	best := pickLRCLIBHit(recs, artist, durationMs)
	if best == nil {
		return Result{}, nil
	}
	return recordResult(*best)
}

// lrclibDo issues a GET with the LRCLIB User-Agent.
func lrclibDo(ctx context.Context, client *http.Client, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return client.Do(req)
}

// pickLRCLIBHit returns the best search hit for the track, or nil. A hit
// must name the artist (its artist field contains ours, case-insensitively,
// as the Genius matcher requires) and, when durationMs is known, be within
// lrclibDurationToleranceMs of it. Among those, synced lyrics beat plain,
// plain beats an instrumental marker, and the first hit wins a tie.
func pickLRCLIBHit(recs []lrclibRecord, artist string, durationMs int) *lrclibRecord {
	want := strings.ToLower(normalizeQuotes(artist))
	var best *lrclibRecord
	bestScore := 0
	for i := range recs {
		r := &recs[i]
		if !strings.Contains(strings.ToLower(normalizeQuotes(r.ArtistName)), want) {
			continue
		}
		if durationMs > 0 {
			diff := r.Duration*1000 - float64(durationMs)
			if diff > lrclibDurationToleranceMs || diff < -lrclibDurationToleranceMs {
				continue
			}
		}
		score := 0
		switch {
		case r.SyncedLyrics != "":
			score = 3
		case r.PlainLyrics != "":
			score = 2
		case r.Instrumental:
			score = 1
		}
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	return best
}

// recordResult maps an LRCLIB record to a Result: ErrInstrumental for an
// instrumental, the synced lyrics when they parse to at least one line,
// else the plain lyrics normalized like Genius text.
func recordResult(rec lrclibRecord) (Result, error) {
	if rec.Instrumental {
		return Result{}, ErrInstrumental
	}
	if lines := parseLRC(rec.SyncedLyrics); len(lines) > 0 {
		return Result{Lines: lines}, nil
	}
	return Result{Lines: plainLines(normalizeLyrics(rec.PlainLyrics))}, nil
}

// roundSeconds converts milliseconds to the nearest whole second.
func roundSeconds(ms int) int {
	return (ms + 500) / 1000
}
