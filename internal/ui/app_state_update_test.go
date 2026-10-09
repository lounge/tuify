package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/testutil"
)

// The tests here run in a synctest bubble. The Model's visualizer fetches
// hit an in-memory server, and the Cmds Update returns are run through
// collectMsgs, so "the Cmd returned" is a state reached through
// synctest.Wait rather than a guess behind a wall-clock timeout.

// volumeAudioSource is an AudioSource that also implements volumeConsumer
// and records every volume the shell pushes to it. Only Update calls it,
// so no locking is needed.
type volumeAudioSource struct {
	volumes []int
}

func (s *volumeAudioSource) Latest() *audio.FrequencyData { return nil }
func (s *volumeAudioSource) SetVolumePercent(v int)       { s.volumes = append(s.volumes, v) }

// newStateTestModel builds a Model whose visualizer fetches (album art,
// lyrics) hit an in-memory 404 server instead of the network. Call it
// inside a synctest bubble: the server runs in the bubble and shuts down
// with it.
func newStateTestModel(t *testing.T, src AudioSource) Model {
	t.Helper()
	srv := httptest.NewTestServer(t, http.NotFoundHandler())
	m := newIntentTestModel()
	m.rootCtx = t.Context()
	m.visualizer = newVisualizerModel(t.Context(), src)
	m.visualizer.httpClient = &http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}
	return m
}

// collectMsgs runs cmd and every Cmd nested in its tea.BatchMsg and
// returns the messages of those that complete. Call it inside a synctest
// bubble: synctest.Wait lets each Cmd run until it has returned or is
// parked on a timer, so the spinner and marquee ticks, which never fire
// here, are left behind rather than waited for. They finish when the
// bubble's clock advances at the end of the test.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := runCmd(cmd)
	synctest.Wait()
	var msg tea.Msg
	select {
	case msg = <-done:
	default:
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func hasMsg(msgs []tea.Msg, want tea.Msg) bool {
	return slices.Contains(msgs, want)
}

func TestHandleStateUpdate_ForwardsVolumeChangesToAudioSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &volumeAudioSource{}
		m := newStateTestModel(t, src)
		m.nowPlaying.trackURI = "spotify:track:x" // same track: no visualizer reload

		send := func(seq uint64, volume int) {
			st := pstate("spotify:track:x", true, 1000)
			st.VolumePercent = volume
			updated, _ := m.Update(playerStateMsg{seq: seq, state: st})
			m = updated.(Model)
		}

		send(1, 100) // unchanged from the default 100
		send(2, 40)
		send(3, 40) // unchanged
		send(4, 75)

		want := []int{40, 75}
		if !slices.Equal(src.volumes, want) {
			t.Errorf("SetVolumePercent calls = %v, want %v (only on change)", src.volumes, want)
		}
	})
}

func TestHandleStateUpdate_TrackChangeSetsWindowTitle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newStateTestModel(t, nil)
		m.nowPlaying.trackURI = "spotify:episode:old"
		want := tea.SetWindowTitle("tuify — Song — Band")()

		st := pstate("spotify:episode:new", true, 0)
		st.TrackName, st.ArtistName = "Song", "Band"
		updated, cmd := m.Update(playerStateMsg{seq: 1, state: st})
		m = updated.(Model)
		if !hasMsg(collectMsgs(cmd), want) {
			t.Error("track change did not emit the window-title Cmd")
		}
		if m.visualizer.trackID != "new" {
			t.Errorf("visualizer trackID = %q, want it re-initialised to %q", m.visualizer.trackID, "new")
		}

		// A further poll of the same item must not retitle the window.
		_, cmd = m.Update(playerStateMsg{seq: 2, state: st})
		if hasMsg(collectMsgs(cmd), want) {
			t.Error("window title re-emitted without a track change")
		}
	})
}

func TestHandleStateUpdate_TrackChangeSyncsCurrentView(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newStateTestModel(t, nil)
		tv := newTrackView(m.rootCtx, m.client, "pl", "PL", 80, 20, false)
		m.viewStack = append(m.viewStack, tv)

		items := []list.Item{
			trackItem{uri: "spotify:track:a", name: "A"},
			trackItem{uri: "spotify:track:b", name: "B"},
			trackItem{uri: "spotify:track:c", name: "C"},
		}
		updated, _ := m.Update(pageLoadedMsg{listID: tv.id, items: items, fetched: 3})
		m = updated.(Model)
		if tv.list.Index() != 0 {
			t.Fatalf("setup: cursor at %d, want 0", tv.list.Index())
		}

		st := pstate("spotify:track:c", true, 0)
		st.ContextURI = tv.contextURI()
		updated, _ = m.Update(playerStateMsg{seq: 1, state: st})
		m = updated.(Model)
		if got := tv.list.Index(); got != 2 {
			t.Errorf("trackView cursor = %d, want 2 (the playing track)", got)
		}

		// Moving the cursor away and polling the same track must not snap back.
		tv.list.Select(0)
		updated, _ = m.Update(playerStateMsg{seq: 2, state: st})
		_ = updated.(Model)
		if got := tv.list.Index(); got != 0 {
			t.Errorf("cursor = %d after a same-track poll, want it left at 0", got)
		}
	})
}

// A track change pages the current list for the playing item only when it
// plays from that list's own context. Otherwise a long playlist would be
// fetched to its end, a request per page, for a track that isn't in it;
// the item is selected only if it is already loaded.
func TestHandleStateUpdate_SyncPagesOnlyInPlayingContext(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		contextURI  string
		wantLoads   int32
		wantIndex   int
		wantSyncURI string
	}{
		{"own context, unloaded track", "spotify:track:zzz", "spotify:playlist:pl", 1, 0, "spotify:track:zzz"},
		{"other context, unloaded track", "spotify:track:zzz", "spotify:playlist:other", 0, 0, ""},
		{"other context, loaded track", "spotify:track:b", "spotify:playlist:other", 0, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var loads atomic.Int32
				load := func(context.Context, int) ([]list.Item, int, bool, error) {
					loads.Add(1)
					return nil, 0, false, nil
				}
				m := newStateTestModel(t, nil)
				tv := &trackView{lazyList: newLazyList(m.rootCtx, load, 80, 20, false), playlistID: "pl", playlistName: "PL"}
				m.viewStack = append(m.viewStack, tv)
				// Enough rows below the cursor that the list's own near-end
				// prefetch stays quiet; any page fetched is the sync's.
				items := make([]list.Item, 20)
				for i := range items {
					items[i] = trackItem{uri: fmt.Sprintf("spotify:track:%c", 'a'+i), name: string(rune('A' + i))}
				}
				updated, _ := m.Update(pageLoadedMsg{listID: tv.id, items: items, fetched: len(items), hasMore: true})
				m = updated.(Model)

				st := pstate(tt.uri, true, 0)
				st.ContextURI = tt.contextURI
				_, cmd := m.Update(playerStateMsg{seq: 1, state: st})
				collectMsgs(cmd)

				if got := loads.Load(); got != tt.wantLoads {
					t.Errorf("pages fetched = %d, want %d", got, tt.wantLoads)
				}
				if tv.loading != (tt.wantLoads > 0) {
					t.Errorf("loading = %v after the sync, want %v", tv.loading, tt.wantLoads > 0)
				}
				if got := tv.list.Index(); got != tt.wantIndex {
					t.Errorf("cursor = %d, want %d", got, tt.wantIndex)
				}
				if tv.syncURI != tt.wantSyncURI {
					t.Errorf("syncURI = %q, want %q", tv.syncURI, tt.wantSyncURI)
				}
			})
		})
	}
}

// After a track from the playlist, an item playing with no context (a queue
// from search) must not inherit the playlist as its context, or the list
// would page to its end for an item that isn't playing from it.
func TestHandleStateUpdate_ContextlessItemDoesNotPagePreviousContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var loads atomic.Int32
		load := func(context.Context, int) ([]list.Item, int, bool, error) {
			loads.Add(1)
			return nil, 0, false, nil
		}
		m := newStateTestModel(t, nil)
		tv := &trackView{lazyList: newLazyList(m.rootCtx, load, 80, 20, false), playlistID: "pl", playlistName: "PL"}
		m.viewStack = append(m.viewStack, tv)
		items := make([]list.Item, 20)
		for i := range items {
			items[i] = trackItem{uri: fmt.Sprintf("spotify:track:%c", 'a'+i), name: string(rune('A' + i))}
		}
		updated, _ := m.Update(pageLoadedMsg{listID: tv.id, items: items, fetched: len(items), hasMore: true})
		m = updated.(Model)

		st := pstate("spotify:track:b", true, 0)
		st.ContextURI = tv.contextURI()
		updated, cmd := m.Update(playerStateMsg{seq: 1, state: st})
		m = updated.(Model)
		collectMsgs(cmd)
		if got := loads.Load(); got != 0 {
			t.Fatalf("setup: %d pages fetched for an already loaded track", got)
		}

		_, cmd = m.Update(playerStateMsg{seq: 2, state: pstate("spotify:track:zzz", true, 0)})
		collectMsgs(cmd)

		if got := loads.Load(); got != 0 {
			t.Errorf("pages fetched = %d, want 0: the contextless item paged the playlist", got)
		}
		if tv.syncURI != "" {
			t.Errorf("syncURI = %q, want none queued", tv.syncURI)
		}
	})
}
