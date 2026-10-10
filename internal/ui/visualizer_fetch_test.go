package ui

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// fetchCounter serves album art at /art/* (blocking until release is
// closed, when set) and answers every lyrics request with 404, counting
// both.
type fetchCounter struct {
	png     []byte
	release chan struct{}
	images  atomic.Int32
	lyrics  atomic.Int32
}

func (f *fetchCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/art/") {
		f.images.Add(1)
		if f.release != nil {
			select {
			case <-f.release:
			case <-r.Context().Done():
				return
			}
		}
		w.Write(f.png)
		return
	}
	f.lyrics.Add(1)
	http.NotFound(w, r)
}

func testTrack(id string) trackInfo {
	return trackInfo{id: id, durationMs: 200_000, track: "Song " + id, artist: "Band", imageURL: "https://img.test/art/" + id}
}

func TestVisualizer_NoFetchWhilePaneClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fetchCounter{png: encodePNG(t, 8, 8)}
		m := newLyricsTestModel(t, f)

		m.setTrack(testTrack("t1"))
		m.setImageURL("https://img.test/art/t1b")
		synctest.Wait()
		if n, l := f.images.Load(), f.lyrics.Load(); n != 0 || l != 0 {
			t.Fatalf("pane closed: %d image and %d lyrics requests, want none", n, l)
		}

		// The shell passes the now-playing track info, art URL included.
		opened := testTrack("t1")
		opened.imageURL = "https://img.test/art/t1b"
		m.toggle(opened)
		synctest.Wait()
		m.drainImages()
		m.drainLyrics()
		if f.images.Load() != 1 || f.lyrics.Load() == 0 {
			t.Errorf("opening the pane: %d image and %d lyrics requests, want the art and the lyrics lookups", f.images.Load(), f.lyrics.Load())
		}
		if _, ok := m.imageCache.get("https://img.test/art/t1b"); !ok {
			t.Error("the art reported while the pane was closed was not loaded on open")
		}

		// With the pane open, a track change fetches straight away.
		before := f.lyrics.Load()
		m.setTrack(testTrack("t2"))
		synctest.Wait()
		if f.images.Load() != 2 || f.lyrics.Load() == before {
			t.Errorf("track change with the pane open: %d image requests, lyrics %d→%d, want both fetched", f.images.Load(), before, f.lyrics.Load())
		}
		m.toggle(trackInfo{})
	})
}

// Closing and reopening the pane while the art is still downloading keeps
// that download instead of cancelling and repeating it.
func TestVisualizer_ToggleKeepsInFlightFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fetchCounter{png: encodePNG(t, 8, 8), release: make(chan struct{})}
		m := newLyricsTestModel(t, f)
		tr := testTrack("t1")

		m.toggle(tr)
		synctest.Wait()
		m.toggle(tr) // close
		m.toggle(tr) // reopen while the art is in flight
		close(f.release)
		synctest.Wait()
		m.drainImages()

		if n := f.images.Load(); n != 1 {
			t.Errorf("%d image requests, want 1 (the in-flight one kept)", n)
		}
		if _, ok := m.imageCache.get(tr.imageURL); !ok {
			t.Error("the kept download did not deliver the art")
		}
	})
}

// An ad or local file puts the pane in its "No track" state instead of
// leaving the previous track's art and lyrics up.
func TestUpdate_NonPlayableItemClearsVisualizer(t *testing.T) {
	m := newIntentTestModel()
	m = applyState(t, m, pstate("spotify:track:a", true, 1000))
	if m.visualizer.trackID != "a" {
		t.Fatalf("setup: visualizer track %q, want a", m.visualizer.trackID)
	}

	m = applyState(t, m, pstate("spotify:local:x", true, 0))
	if m.visualizer.trackID != "" || m.visualizer.imageURL != "" {
		t.Errorf("after a local file: trackID=%q imageURL=%q, want cleared", m.visualizer.trackID, m.visualizer.imageURL)
	}
	if got := m.visualizer.View(40, 5); !strings.Contains(got, "No track") {
		t.Errorf("visualizer view = %q, want the No track state", got)
	}
}
