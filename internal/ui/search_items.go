package ui

import (
	"fmt"
	"strings"
)

// albumItem represents an album in a list.
type albumItem struct {
	id          string
	uri         string
	name        string
	artist      string
	releaseDate string
	trackCount  int
}

func (i albumItem) Title() string { return i.name }
func (i albumItem) Description() string {
	year := i.releaseDate
	if len(year) >= 4 {
		year = year[:4]
	}
	return fmt.Sprintf("%s · %s · %d tracks", i.artist, year, i.trackCount)
}
func (i albumItem) FilterValue() string { return i.name }
func (i albumItem) URI() string         { return i.uri }

// artistItem represents an artist in a list.
type artistItem struct {
	id     string
	uri    string
	name   string
	genres []string
}

func (i artistItem) Title() string { return i.name }
func (i artistItem) Description() string {
	if len(i.genres) == 0 {
		return ""
	}
	return strings.Join(i.genres, ", ")
}
func (i artistItem) FilterValue() string { return i.name }
func (i artistItem) URI() string         { return i.uri }
