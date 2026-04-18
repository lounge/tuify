// Package lyrics fetches song lyrics for the visualizer's lyrics panel.
//
// Strategy: scrape genius.com's search results for the best matching
// track, follow the result URL, and extract lyric text from the rendered
// HTML. No API key is required (Genius's public API demands one for any
// useful endpoint, so scraping is the pragmatic choice for a user-local
// TUI).
//
// Search returns ErrInstrumental when Genius's page indicates the track
// has no lyrics (e.g. instrumental releases), so callers can render an
// "Instrumental" marker instead of "Lyrics not found". Network and parse
// failures surface as ordinary errors.
package lyrics
