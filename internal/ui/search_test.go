package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

func TestParseSearch(t *testing.T) {
	tests := []struct {
		input      string
		wantPrefix searchPrefix
		wantTerm   string
	}{
		{"queen", prefixTrack, "queen"},
		{"t:queen", prefixTrack, "queen"},
		{"e:podcast name", prefixEpisode, "podcast name"},
		{"l:album name", prefixAlbum, "album name"},
		{"a:artist name", prefixArtist, "artist name"},
		{"s:show name", prefixShow, "show name"},
		{"x:unknown", prefixTrack, "x:unknown"},
		{"", prefixTrack, ""},
		{":", prefixTrack, ":"},
		{"a:", prefixArtist, ""},
		{"t:colon:in:term", prefixTrack, "colon:in:term"},
	}

	for _, tt := range tests {
		prefix, term := parseSearch(tt.input)
		if prefix != tt.wantPrefix || term != tt.wantTerm {
			t.Errorf("parseSearch(%q) = (%v, %q), want (%v, %q)",
				tt.input, prefix, term, tt.wantPrefix, tt.wantTerm)
		}
	}
}

func TestQueueFrom_Basic(t *testing.T) {
	v := searchView{
		items: []list.Item{
			trackItem{uri: "u1", name: "A"},
			trackItem{uri: "u2", name: "B"},
			trackItem{uri: "u3", name: "C"},
			trackItem{uri: "u4", name: "D"},
		},
	}

	uris := v.queueFrom("u2")
	if len(uris) != 3 {
		t.Fatalf("expected 3 URIs, got %d", len(uris))
	}
	if uris[0] != "u2" || uris[1] != "u3" || uris[2] != "u4" {
		t.Errorf("unexpected URIs: %v", uris)
	}
}

func TestQueueFrom_FirstItem(t *testing.T) {
	v := searchView{
		items: []list.Item{
			trackItem{uri: "u1", name: "A"},
			trackItem{uri: "u2", name: "B"},
		},
	}

	uris := v.queueFrom("u1")
	if len(uris) != 2 {
		t.Fatalf("expected 2 URIs, got %d", len(uris))
	}
}

func TestQueueFrom_LastItem(t *testing.T) {
	v := searchView{
		items: []list.Item{
			trackItem{uri: "u1", name: "A"},
			trackItem{uri: "u2", name: "B"},
		},
	}

	uris := v.queueFrom("u2")
	if len(uris) != 1 || uris[0] != "u2" {
		t.Errorf("expected [u2], got %v", uris)
	}
}

func TestQueueFrom_NotFound(t *testing.T) {
	v := searchView{
		items: []list.Item{
			trackItem{uri: "u1", name: "A"},
		},
	}

	uris := v.queueFrom("u99")
	if len(uris) != 0 {
		t.Errorf("expected empty, got %v", uris)
	}
}

func TestQueueFrom_MaxCap(t *testing.T) {
	var items []list.Item
	for i := 0; i < 100; i++ {
		items = append(items, trackItem{uri: "u", name: "t"})
	}
	v := searchView{items: items}

	uris := v.queueFrom("u")
	if len(uris) != maxQueueURIs {
		t.Errorf("expected %d URIs (max), got %d", maxQueueURIs, len(uris))
	}
}

func TestQueueFrom_SkipsNonURIItems(t *testing.T) {
	v := searchView{
		items: []list.Item{
			trackItem{uri: "u1", name: "A"},
			statusItem{text: "Loading..."},
			trackItem{uri: "u2", name: "B"},
		},
	}

	uris := v.queueFrom("u1")
	if len(uris) != 2 {
		t.Fatalf("expected 2 URIs (skipping statusItem), got %d", len(uris))
	}
	if uris[0] != "u1" || uris[1] != "u2" {
		t.Errorf("unexpected URIs: %v", uris)
	}
}

func TestSearchView_IsPlayable(t *testing.T) {
	tests := []struct {
		prefix searchPrefix
		depth  int
		want   bool
	}{
		{prefixTrack, 0, true},
		{prefixEpisode, 0, true},
		{prefixAlbum, 0, false},
		{prefixAlbum, 1, true},
		{prefixShow, 0, false},
		{prefixShow, 1, true},
		{prefixArtist, 0, false},
		{prefixArtist, 1, false},
		{prefixArtist, 2, true},
	}

	for _, tt := range tests {
		v := searchView{prefix: tt.prefix, depth: tt.depth}
		if got := v.isPlayable(); got != tt.want {
			t.Errorf("isPlayable(prefix=%d, depth=%d) = %v, want %v",
				tt.prefix, tt.depth, got, tt.want)
		}
	}
}

func TestSearchView_ContextURI(t *testing.T) {
	v := searchView{
		prefix:        prefixAlbum,
		depth:         1,
		selectedAlbum: selectedRef{id: "a1", uri: "spotify:album:a1", name: "Album"},
	}
	if got := v.contextURI(); got != "spotify:album:a1" {
		t.Errorf("contextURI for album depth 1: got %q", got)
	}

	v2 := searchView{
		prefix:       prefixShow,
		depth:        1,
		selectedShow: selectedRef{id: "s1", uri: "spotify:show:s1", name: "Show"},
	}
	if got := v2.contextURI(); got != "spotify:show:s1" {
		t.Errorf("contextURI for show depth 1: got %q", got)
	}

	v3 := searchView{
		prefix:        prefixArtist,
		depth:         2,
		selectedAlbum: selectedRef{id: "a2", uri: "spotify:album:a2", name: "Album"},
	}
	if got := v3.contextURI(); got != "spotify:album:a2" {
		t.Errorf("contextURI for artist depth 2: got %q", got)
	}

	v4 := searchView{prefix: prefixTrack, depth: 0}
	if got := v4.contextURI(); got != "" {
		t.Errorf("contextURI for track depth 0: got %q, want empty", got)
	}
}

func TestSearchView_Breadcrumb(t *testing.T) {
	tests := []struct {
		name string
		v    searchView
		want string
	}{
		{
			"search root",
			searchView{prefix: prefixTrack, depth: 0},
			"Home > Search",
		},
		{
			"artist depth 1",
			searchView{
				prefix:         prefixArtist,
				depth:          1,
				selectedArtist: selectedRef{id: "a1", name: "Queen"},
			},
			"Home > Search > Queen",
		},
		{
			"artist depth 2",
			searchView{
				prefix:         prefixArtist,
				depth:          2,
				selectedArtist: selectedRef{id: "a1", name: "Queen"},
				selectedAlbum:  selectedRef{id: "al1", uri: "u", name: "A Night at the Opera"},
			},
			"Home > Search > Queen > A Night at the Opera",
		},
		{
			"album depth 1",
			searchView{
				prefix:        prefixAlbum,
				depth:         1,
				selectedAlbum: selectedRef{id: "al1", uri: "u", name: "Dark Side"},
			},
			"Home > Search > Dark Side",
		},
		{
			"show depth 1",
			searchView{
				prefix:       prefixShow,
				depth:        1,
				selectedShow: selectedRef{id: "s1", uri: "u", name: "My Podcast"},
			},
			"Home > Search > My Podcast",
		},
	}

	for _, tt := range tests {
		got := tt.v.Breadcrumb()
		if got != tt.want {
			t.Errorf("%s: Breadcrumb() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
