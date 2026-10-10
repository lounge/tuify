// Package lyrics fetches song lyrics for the visualizer's lyrics panel.
//
// Strategy: ask LRCLIB (lrclib.net, open data, no key) first and fall
// back to Genius. Search runs three lookups in order and stops at the
// first that yields lyrics:
//
//  1. LRCLIB /api/get: an exact title and artist match sent with the
//     track's duration, which picks the right edit when the catalogue
//     holds several lengths of a song. A 404 is a miss.
//  2. LRCLIB /api/search: a fuzzy query with the cleaned title (remix,
//     remaster and feat. suffixes stripped), filtered to hits that name
//     the artist and are within about 2 s of the duration, preferring
//     synced lyrics over plain and either over an instrumental marker.
//  3. Genius: the keyless search endpoint (genius.com/api/search) for the
//     best matching song, then the lyric text extracted from that song's
//     page. Genius's documented API needs a key for any useful endpoint,
//     so scraping is the pragmatic choice for a user-local TUI.
//
// An LRCLIB failure (network, 5xx, bad JSON, which under encoding/json/v2
// includes invalid UTF-8 or a duplicated key) is logged and treated as a
// miss so Genius still runs; only Genius's error is returned. Every
// fall-through to Genius is logged, so whether Genius is still worth
// keeping can be judged from use.
//
// Search returns a Result of Lines. LRCLIB's syncedLyrics is LRC text
// ([mm:ss.xx] stamps; [mm:ss] and [mm:ss.xxx] are accepted too). It is
// parsed into Lines with StartMs set and sorted by time, one Line per
// stamp when a line carries several, with stamped empty lines kept as
// instrumental breaks and metadata tags dropped. Plain lyrics from either
// source become Lines with StartMs -1. A Result is all timed or all
// untimed; Result.Synced tells which. LRCLIB also returns an undocumented
// lyricsfile YAML field, which is ignored.
//
// Search returns ErrInstrumental when a source marks the song as
// instrumental, so callers can render an "Instrumental" marker instead of
// "Lyrics not found". Network and parse failures surface as ordinary
// errors.
//
// LRCLIB requests carry a User-Agent naming tuify, as its docs ask. The
// Genius song URL in a search hit is followed only when it points at
// genius.com or a subdomain over https; any other scheme or host is an
// error, since the URL comes from the response itself. Every response is
// read up to 4 MiB and cut off beyond that.
//
// Returned text has been passed through termsafe.Clean line by line: both
// sources are crowd-edited, and Genius HTML entities decode to raw control
// characters.
package lyrics
