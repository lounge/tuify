package ui

import "testing"

// A page of only withheld episodes, which the client drops, must still
// move the offset by its size, or the next request asks for the same page.
func TestEpisodeView_PagesPastWithheldEpisodes(t *testing.T) {
	client := newWithheldPagesClient(t, nil,
		map[string]any{"id": "e3", "uri": "spotify:episode:e3", "name": "Three"})
	v := newEpisodeView(t.Context(), client, "show", "Show", 80, 20, false)

	pageWithFilterOpen(t, &v.lazyList)
	if got := uris(v.items); len(got) != 1 || got[0] != "spotify:episode:e3" || v.offset != 3 {
		t.Errorf("items %v offset %d, want the episode after the withheld page and offset 3", got, v.offset)
	}
}
