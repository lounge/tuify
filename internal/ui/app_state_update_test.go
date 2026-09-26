package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/testutil"
)

// volumeAudioSource is an AudioSource that also implements volumeConsumer
// and records every volume the shell pushes to it. Only Update calls it,
// so no locking is needed.
type volumeAudioSource struct {
	volumes []int
}

func (s *volumeAudioSource) Latest() *audio.FrequencyData { return nil }
func (s *volumeAudioSource) SetVolumePercent(v int)       { s.volumes = append(s.volumes, v) }

// newStateTestModel builds a Model whose visualizer fetches (album art,
// lyrics) hit a local 404 server instead of the network.
func newStateTestModel(t *testing.T, src AudioSource) Model {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	m := newIntentTestModel()
	m.rootCtx = t.Context()
	m.visualizer = newVisualizerModel(t.Context(), src)
	m.visualizer.httpClient = &http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}
	return m
}

// collectMsgs runs cmd and every Cmd nested in a tea.BatchMsg, returning
// the messages that are produced promptly. Timer-driven Cmds (spinner and
// marquee ticks) don't return within the window and are left behind.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(50 * time.Millisecond):
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
	for _, m := range msgs {
		if m == want {
			return true
		}
	}
	return false
}

func TestHandleStateUpdate_ForwardsVolumeChangesToAudioSource(t *testing.T) {
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
	if len(src.volumes) != len(want) || src.volumes[0] != want[0] || src.volumes[1] != want[1] {
		t.Errorf("SetVolumePercent calls = %v, want %v (only on change)", src.volumes, want)
	}
}

func TestHandleStateUpdate_TrackChangeSetsWindowTitle(t *testing.T) {
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
}

func TestHandleStateUpdate_TrackChangeSyncsCurrentView(t *testing.T) {
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
}
