package ui

import (
	"image/color"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/lounge/tuify/internal/spotify"
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

// Nothing playing anywhere (HTTP 204) keeps trackURI, so the URI-change
// check left the last track's art and lyrics in the pane while the bar
// said "No track playing". Playback coming back to that same track must
// put its art and lyrics back, and the pane must still close with v.
func TestUpdate_NothingPlayingClearsVisualizer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fetchCounter{png: encodeColorPNG(t, 8, 8, color.RGBA{R: 200, G: 40, B: 40, A: 255})}
		const cover = "200;40;40" // the cover's colour in the frame's escapes
		m := newIntentTestModel()
		m.visualizer = newLyricsTestModel(t, f)
		playing := func(progressMs int) *spotify.PlayerState {
			s := pstate("spotify:track:a", true, progressMs)
			s.ImageURL = "https://img.test/art/a"
			return s
		}
		// shown lets the fetches land and runs the cover's 150-frame
		// dissolve to the end, then returns the art and lyrics frames.
		shown := func() (art, words string) {
			synctest.Wait()
			for range 200 {
				m.visualizer.advance(0)
			}
			return m.visualizer.View(40, 10), lyricsViz(t, m.visualizer).View(40, 10)
		}

		m = applyState(t, m, playing(1000))
		m, _ = pressKeys(t, m, runeKey("v"))
		art, words := shown()
		if !m.visualizer.active || !strings.Contains(art, cover) {
			t.Fatalf("setup: pane active=%v, cover drawn=%v, want the open pane showing it", m.visualizer.active, strings.Contains(art, cover))
		}

		m = applyState(t, m, nil)
		if m.visualizer.trackID != "" {
			t.Errorf("nothing playing: visualizer track %q, want cleared", m.visualizer.trackID)
		}
		if got := m.visualizer.View(40, 5); !strings.Contains(got, "No track") {
			t.Errorf("visualizer view = %q, want the No track state", got)
		}

		m = applyState(t, m, playing(2000))
		if gotArt, gotWords := shown(); gotArt != art || gotWords != words {
			t.Errorf("playback back on the same track: art restored=%v, lyrics restored=%v", gotArt == art, gotWords == words)
		}
		if n := f.images.Load(); n != 1 {
			t.Errorf("%d art downloads, want 1 (the cached cover reused)", n)
		}

		m = applyState(t, m, nil)
		m, _ = pressKeys(t, m, runeKey("v"))
		if m.visualizer.active {
			t.Error("v did not close the pane while nothing is playing")
		}
		synctest.Wait()
	})
}
