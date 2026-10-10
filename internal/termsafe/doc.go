// Package termsafe strips terminal control characters from text that comes
// from outside the app before it can reach the screen.
//
// Track, playlist, artist, show and device names come from Spotify, where
// anyone can name a playlist or a podcast, and lyrics come from LRCLIB
// and Genius, where anyone can edit them. The TUI writes that text straight into the
// terminal, and lipgloss passes escape sequences through, so an ESC inside
// a name would be executed rather than shown: an OSC 52 sequence can write
// the clipboard, others retitle the window, clear the screen or plant
// hyperlinks.
//
// Clean is applied where the spotify and lyrics packages map API responses
// into their own types, so everything above them can treat those strings
// as plain text. It is not applied in View; rendering stays free of it.
package termsafe
