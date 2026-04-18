package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

// searchPrefix identifies the type of search.
type searchPrefix int

const (
	prefixTrack   searchPrefix = iota // default / "t:"
	prefixEpisode                     // "e:"
	prefixAlbum                       // "l:"
	prefixArtist                      // "a:"
	prefixShow                        // "s:"
)

var searchHintText = strings.Join([]string{
	helpCmdStyle.Render("t:") + helpDescStyle.Render("  track search (default)"),
	helpCmdStyle.Render("e:") + helpDescStyle.Render("  episode search"),
	helpCmdStyle.Render("a:") + helpDescStyle.Render("  artist → album → track"),
	helpCmdStyle.Render("l:") + helpDescStyle.Render("  album → track"),
	helpCmdStyle.Render("s:") + helpDescStyle.Render("  show → episode"),
}, "\n")

// parseSearch splits input into prefix + term. Returns prefixTrack for
// unrecognised prefixes (the whole string becomes the term).
func parseSearch(input string) (searchPrefix, string) {
	if idx := strings.Index(input, ":"); idx > 0 {
		switch input[:idx] {
		case "t":
			return prefixTrack, input[idx+1:]
		case "e":
			return prefixEpisode, input[idx+1:]
		case "l":
			return prefixAlbum, input[idx+1:]
		case "a":
			return prefixArtist, input[idx+1:]
		case "s":
			return prefixShow, input[idx+1:]
		}
	}
	return prefixTrack, input
}

// Messages

type searchDebounceMsg struct {
	seq   int
	query string
}

type searchResultMsg struct {
	items   []list.Item
	hasMore bool
	query   string
	epoch   int
	err     error
}

// selectedRef is a drill-down selection pinned across depth levels.
type selectedRef struct {
	id, uri, name string
}
