package ui

import "testing"

// A page of only withheld shows, which the client drops, must still move
// the offset by its size, or the next request asks for the same page.
func TestPodcastView_PagesPastWithheldShows(t *testing.T) {
	client := newWithheldPagesClient(t, map[string]any{"show": nil},
		map[string]any{"show": map[string]any{"id": "s3", "uri": "spotify:show:s3", "name": "Three"}})
	v := newPodcastView(t.Context(), client, 80, 20, false)

	pageWithFilterOpen(t, &v.lazyList)
	if got := uris(v.items); len(got) != 1 || got[0] != "spotify:show:s3" || v.offset != 3 {
		t.Errorf("items %v offset %d, want the show after the withheld page and offset 3", got, v.offset)
	}
}
