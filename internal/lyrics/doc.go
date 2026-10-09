// Package lyrics fetches song lyrics for the visualizer's lyrics panel.
//
// Strategy: query genius.com's keyless JSON search endpoint
// (genius.com/api/search) for the best matching song, then fetch that
// song's page and extract the lyric text from the rendered HTML. Genius's
// documented API needs a key for any useful endpoint, so this is the
// pragmatic choice for a user-local TUI.
//
// Search returns ErrInstrumental when the search result marks the song as
// instrumental, so callers can render an "Instrumental" marker instead of
// "Lyrics not found". Network and parse failures surface as ordinary
// errors.
//
// The song URL in a search hit is followed only when it points at
// genius.com or a subdomain over https; any other scheme or host is an
// error, since the URL comes from the response itself. Both the search
// response and the song page are read up to 4 MiB and cut off beyond
// that.
//
// Returned lyrics have been passed through termsafe.Clean line by line:
// Genius text is crowd-edited, and HTML entities decode to raw control
// characters.
package lyrics
