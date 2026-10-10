package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// A playlist page can hold entries without a track (local files, removed
// tracks) that the client drops. The offset must still advance by the raw
// page size, or the next fetch re-reads the dropped rows and repeats the
// tracks after them; a page of only dropped rows would never advance.
func TestTrackView_AdvancesOffsetByRawPageSize(t *testing.T) {
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, map[string]any{
			"offset": 0, "total": 60,
			"items": []map[string]any{
				{"item": map[string]any{"id": "t1", "uri": "spotify:track:t1", "name": "One"}},
				{"item": map[string]any{"id": "", "uri": "", "name": ""}},
				{"item": map[string]any{"id": "t3", "uri": "spotify:track:t3", "name": "Three"}},
			},
		})
	}))
	client := spotify.New(&http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}})
	tv := newTrackView(context.Background(), client, "pl", "PL", 80, 20, false)

	page, ok := tv.fetchMore()().(pageLoadedMsg)
	if !ok {
		t.Fatal("fetchMore did not produce a pageLoadedMsg")
	}
	if page.err != nil {
		t.Fatalf("page load: %v", page.err)
	}
	if len(page.items) != 2 || page.fetched != 3 {
		t.Fatalf("items=%d fetched=%d, want 2 items from a 3-entry page", len(page.items), page.fetched)
	}
	tv.Update(page)
	if tv.offset != 3 {
		t.Errorf("offset after the page = %d, want 3 (raw page size)", tv.offset)
	}
}
