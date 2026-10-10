package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/lounge/tuify/internal/testutil"
	"github.com/lounge/tuify/internal/ui/visualizers"
)

// newLyricsTestModel builds a visualizerModel without an audio source
// (album art and lyrics only) whose fetches hit the given in-memory
// handler. Call it inside a synctest bubble: the server runs in the bubble
// and shuts down with it.
func newLyricsTestModel(t *testing.T, h http.Handler) *visualizerModel {
	t.Helper()
	srv := httptest.NewTestServer(t, h)
	m := newVisualizerModel(t.Context(), nil)
	m.httpClient = &http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}
	return m
}

// lyricsViz returns the Lyrics visualizer from the model's list.
func lyricsViz(t *testing.T, m *visualizerModel) *visualizers.Lyrics {
	t.Helper()
	for _, v := range m.vizList {
		if l, ok := v.(*visualizers.Lyrics); ok {
			return l
		}
	}
	t.Fatal("no Lyrics visualizer in the list")
	return nil
}

// A synced LRCLIB hit is cached under the track ID with its timestamps,
// the request carries the track duration in whole seconds, and the Lyrics
// visualizer receives the lines.
func TestLoadLyrics_CachesSyncedLinesAndSendsDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var gotDuration string
		m := newLyricsTestModel(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/get" {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			gotDuration = r.URL.Query().Get("duration")
			mu.Unlock()
			json.MarshalWrite(w, map[string]any{
				"artistName":   "Band",
				"duration":     200.0,
				"instrumental": false,
				"plainLyrics":  "first\nsecond",
				"syncedLyrics": "[00:01.00] first\n[00:05.00] second",
			})
		}))

		m.active = true
		m.setTrack(trackInfo{id: "t1", durationMs: 200400, track: "Song", artist: "Band"})
		synctest.Wait()
		m.drainLyrics()

		cached, ok := m.lyricsCache.get("t1")
		if !ok {
			t.Fatal("lyrics were not cached")
		}
		want := []visualizers.LyricLine{{StartMs: 1000, Text: "first"}, {StartMs: 5000, Text: "second"}}
		if cached.instrumental || len(cached.lines) != 2 || cached.lines[0] != want[0] || cached.lines[1] != want[1] {
			t.Errorf("cached = %+v, want lines %v", cached, want)
		}
		mu.Lock()
		defer mu.Unlock()
		if gotDuration != "200" {
			t.Errorf("duration query = %q, want 200", gotDuration)
		}

		viz := lyricsViz(t, m)
		viz.SetProgress(1000)
		if got := viz.View(40, 3); !strings.Contains(got, "first") {
			t.Errorf("Lyrics visualizer did not receive the lines:\n%s", got)
		}
	})
}

// A failing fetch (every source answering 5xx) shows "No lyrics found"
// for this track but is not cached, so the next visit retries.
func TestLoadLyrics_FetchErrorIsNotCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newLyricsTestModel(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))

		m.active = true
		m.setTrack(trackInfo{id: "t1", durationMs: 200000, track: "Song", artist: "Band"})
		synctest.Wait()
		m.drainLyrics()

		if _, ok := m.lyricsCache.get("t1"); ok {
			t.Error("a failed fetch was cached")
		}
		if got := lyricsViz(t, m).View(40, 3); !strings.Contains(got, "No lyrics found") {
			t.Errorf("Lyrics visualizer after a failed fetch:\n%s", got)
		}
	})
}

// Nothing found anywhere is a result worth caching: the track will not
// grow lyrics between visits, and refetching would hit three services.
func TestLoadLyrics_NotFoundIsCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newLyricsTestModel(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/search" && r.URL.Query().Has("track_name"):
				w.Write([]byte(`[]`)) // LRCLIB search: no hits
			case r.URL.Path == "/api/search":
				w.Write([]byte(`{"meta":{"status":200},"response":{"hits":[]}}`)) // Genius: no hits
			default:
				http.NotFound(w, r) // LRCLIB get
			}
		}))

		m.active = true
		m.setTrack(trackInfo{id: "t1", durationMs: 200000, track: "Song", artist: "Band"})
		synctest.Wait()
		m.drainLyrics()

		cached, ok := m.lyricsCache.get("t1")
		if !ok {
			t.Fatal("a not-found result was not cached")
		}
		if cached.instrumental || cached.lines != nil {
			t.Errorf("cached = %+v, want an empty entry", cached)
		}
	})
}
