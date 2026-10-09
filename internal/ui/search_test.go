package ui

import (
	"context"
	"strings"
	"testing"
	"time"

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
	for range 100 {
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

// syncTo pages for the playing item only when it plays from the album or
// show this view drilled into. Another context selects among the loaded
// results and stops, as do depth-0 results, which play as a queue and have
// no context of their own.
func TestSearchView_SyncTo_PagesOnlyInOwnContext(t *testing.T) {
	loaded := []list.Item{
		trackItem{uri: "spotify:track:t1", name: "One"},
		trackItem{uri: "spotify:track:t2", name: "Two"},
	}
	newAlbumTracks := func() *searchView {
		v := newSearchView(context.Background(), nil, 80, 20, false)
		v.prefix = prefixAlbum
		v.depth = 1
		v.selectedAlbum = selectedRef{id: "a1", uri: "spotify:album:a1", name: "Album"}
		v.items = loaded
		v.list.SetItems(v.items)
		v.hasMore = true
		return v
	}
	tests := []struct {
		name        string
		uri         string
		contextURI  string
		wantFetch   bool
		wantIndex   int
		wantSyncURI string
	}{
		{"own context, unloaded track", "spotify:track:t9", "spotify:album:a1", true, 0, "spotify:track:t9"},
		{"other context, unloaded track", "spotify:track:t9", "spotify:playlist:p", false, 0, ""},
		{"other context, loaded track", "spotify:track:t2", "spotify:playlist:p", false, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newAlbumTracks()
			cmd := v.syncTo(tt.uri, tt.contextURI)
			if (cmd != nil) != tt.wantFetch || (v.pending > 0) != tt.wantFetch {
				t.Errorf("fetch issued = %v (pending %d), want %v", cmd != nil, v.pending, tt.wantFetch)
			}
			if got := v.list.Index(); got != tt.wantIndex {
				t.Errorf("cursor = %d, want %d", got, tt.wantIndex)
			}
			if v.syncURI != tt.wantSyncURI {
				t.Errorf("syncURI = %q, want %q", v.syncURI, tt.wantSyncURI)
			}
		})
	}

	t.Run("depth-0 results never page", func(t *testing.T) {
		v := newSearchView(context.Background(), nil, 80, 20, false)
		v.items = loaded
		v.list.SetItems(v.items)
		v.hasMore = true
		if cmd := v.syncTo("spotify:track:t9", ""); cmd != nil || v.pending > 0 {
			t.Errorf("track results paged for an unloaded item (pending %d)", v.pending)
		}
	})
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
		got := tt.v.breadcrumb()
		if got != tt.want {
			t.Errorf("%s: breadcrumb() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFetchCmd_AppliesListFetchTimeout(t *testing.T) {
	var gotDeadline time.Time
	var hadDeadline bool
	cmd := fetchCmd(t.Context(), 1, "q",
		func(ctx context.Context) ([]string, bool, error) {
			gotDeadline, hadDeadline = ctx.Deadline()
			return nil, false, nil
		},
		func(s string) list.Item { return statusItem{text: s} },
	)
	start := time.Now()
	cmd()
	end := time.Now()
	if !hadDeadline {
		t.Fatal("fetch ran without a deadline")
	}
	// The deadline is set inside cmd, so it falls in [start, end] + timeout.
	if gotDeadline.Before(start.Add(listFetchTimeout)) || gotDeadline.After(end.Add(listFetchTimeout)) {
		t.Errorf("deadline %v, want listFetchTimeout (%v) after the fetch started", gotDeadline.Sub(start), listFetchTimeout)
	}
}

func TestSearchHintText_UsesThemeStyles(t *testing.T) {
	withTrueColor(t)
	if got := searchHintText(); !strings.Contains(got, "\x1b[") {
		t.Errorf("search hint has no colour escapes; styles were captured before RebuildStyles: %q", got)
	}
}

func TestSearchView_IgnoresResultFromEarlierSearchView(t *testing.T) {
	old := newSearchView(context.Background(), nil, 80, 20, false)
	cur := newSearchView(context.Background(), nil, 80, 20, false)
	cur.pending = 1
	cur.list.SetItems([]list.Item{loadingStatusItem})

	cur.Update(searchResultMsg{epoch: old.epoch, items: []list.Item{trackItem{uri: "spotify:track:old"}}})

	if cur.pending != 1 || len(cur.items) != 0 {
		t.Errorf("result from an earlier search view was applied: pending=%d items=%d", cur.pending, len(cur.items))
	}
}
