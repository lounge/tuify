package ui

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// playerStub serves the device list and records every player command it
// receives as "METHOD /path?device_id=…".
type playerStub struct {
	devices []map[string]any
	mu      sync.Mutex
	calls   []string
}

func (s *playerStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/me/player/devices" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"devices": s.devices})
		return
	}
	s.mu.Lock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path+"?device_id="+r.URL.Query().Get("device_id"))
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *playerStub) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// newPlayerStubModel returns a Model whose client talks to stub and
// prefers the device named "tuify".
func newPlayerStubModel(t *testing.T, stub *playerStub) Model {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	client := spotify.New(&http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}}, spotify.WithPreferredDevice("tuify"))
	m := newIntentTestModel()
	m.client = client
	m.rootCtx = t.Context()
	m.nowPlaying = newNowPlaying(t.Context(), client)
	return m
}

// applyState feeds one poll reply through Model.Update with a fresh
// sequence number.
func applyState(t *testing.T, m Model, state *spotify.PlayerState) Model {
	t.Helper()
	m.nowPlaying.pollSeq++
	updated, _ := m.Update(playerStateMsg{seq: m.nowPlaying.pollSeq, state: state})
	return updated.(Model)
}

// runPlayback runs a playback command and returns its result.
func runPlayback(t *testing.T, cmd tea.Cmd) playbackResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no playback command")
	}
	res, ok := cmd().(playbackResultMsg)
	if !ok {
		t.Fatal("command did not produce a playbackResultMsg")
	}
	return res
}

// After playback ends (HTTP 204) the player is not playing, so space must
// resume rather than pause a stopped player.
func TestSpaceAfterNothingPlayingSendsResume(t *testing.T) {
	stub := &playerStub{devices: []map[string]any{
		{"id": "d1", "name": "tuify", "type": "Computer", "is_active": true},
	}}
	m := newPlayerStubModel(t, stub)
	m = applyState(t, m, withDevice(pstate("spotify:track:x", true, 1000), "tuify"))
	m = applyState(t, m, nil)
	if m.nowPlaying.playing || m.nowPlaying.hasTrack {
		t.Fatalf("after 204: playing=%v hasTrack=%v, want both false", m.nowPlaying.playing, m.nowPlaying.hasTrack)
	}

	m, cmd := pressKeys(t, m, keyMsg(" "))
	if res := runPlayback(t, cmd); res.err != nil {
		t.Fatalf("resume failed: %v", res.err)
	}
	if got := stub.recorded(); !slices.Equal(got, []string{"PUT /v1/me/player/play?device_id=d1"}) {
		t.Errorf("calls = %v, want a single resume (play) on d1", got)
	}
	if !m.nowPlaying.playing {
		t.Error("space after a stop should optimistically show playing")
	}
}

// With playback moved to a phone the override is set. Once nothing plays
// anywhere there is no active device to target, so the override clears
// and a play command goes to the preferred device instead of failing the
// active-only lookup. A later poll naming another device re-arms it.
func TestDeviceOverrideClearsWhenNothingPlays(t *testing.T) {
	stub := &playerStub{devices: []map[string]any{
		{"id": "d1", "name": "tuify", "type": "Computer", "is_active": false},
		{"id": "d2", "name": "Phone", "type": "Smartphone", "is_active": false},
	}}
	m := newPlayerStubModel(t, stub)
	m = applyState(t, m, withDevice(pstate("spotify:track:x", true, 1000), "Phone"))
	wantOverride(t, m.nowPlaying, true)

	m = applyState(t, m, nil)
	wantOverride(t, m.nowPlaying, false)

	res := runPlayback(t, m.playItem("spotify:track:y", ""))
	if res.err != nil {
		t.Fatalf("play after 204 failed: %v (the override would have required an active device)", res.err)
	}
	calls := stub.recorded()
	if len(calls) == 0 || calls[len(calls)-1] != "PUT /v1/me/player/play?device_id=d1" {
		t.Errorf("calls = %v, want the play to target the preferred device d1", calls)
	}

	m = applyState(t, m, withDevice(pstate("spotify:track:y", true, 0), "Phone"))
	wantOverride(t, m.nowPlaying, true)
}

func TestSeekKeyWithoutTrackIsNoOp(t *testing.T) {
	m := newIntentTestModel()
	m.nowPlaying.durationMs = 60000
	for _, key := range []string{"d", "a"} {
		after, cmd := pressKeys(t, m, runeKey(key))
		if cmd != nil {
			t.Errorf("%s with no track returned a command", key)
		}
		if after.nowPlaying.seekPending || after.seekSeq != 0 {
			t.Errorf("%s with no track: seekPending=%v seekSeq=%d, want untouched", key, after.nowPlaying.seekPending, after.seekSeq)
		}
	}
}

// A user seek supersedes a cached episode position the resume guard is
// waiting for; afterwards polls must show the real position.
func TestSeekClearsResumeGuard(t *testing.T) {
	m := newIntentTestModel()
	m.nowPlaying.hasTrack, m.nowPlaying.trackURI = true, "spotify:episode:a"
	m.nowPlaying.durationMs, m.nowPlaying.progressMs = 300_000, 120_000
	m.nowPlaying.resumeUntilMs = 120_000

	m, _ = pressKeys(t, m, runeKey("a"))
	if m.nowPlaying.resumeUntilMs != 0 {
		t.Errorf("resumeUntilMs = %d after a seek, want 0", m.nowPlaying.resumeUntilMs)
	}
	m.nowPlaying.seekPending = false // the seek's reply arrived
	m = applyState(t, m, pstate("spotify:episode:a", true, 10_000))
	if m.nowPlaying.progressMs != 10_000 {
		t.Errorf("progressMs = %d, want the polled 10000", m.nowPlaying.progressMs)
	}
}

// When the resume seek fails the player stays at the start; the guard must
// not keep hiding that until playback reaches the cached position.
func TestFailedSeekClearsResumeGuard(t *testing.T) {
	m := newIntentTestModel()
	m.nowPlaying.hasTrack, m.nowPlaying.trackURI = true, "spotify:episode:a"
	m.nowPlaying.resumeUntilMs = 120_000

	updated, _ := m.Update(playbackResultMsg{op: opSeek, err: errTest})
	m = updated.(Model)
	if m.nowPlaying.resumeUntilMs != 0 {
		t.Errorf("resumeUntilMs = %d after a failed seek, want 0", m.nowPlaying.resumeUntilMs)
	}
}

// Two quick presses of space: the first reply fails after the second press.
// Reverting must not undo the second press's flip.
func TestPlaybackResultRevertsOnlyItsOwnPress(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		op      playbackOp
		pending func(np *nowPlayingModel) uint64
		state   func(np *nowPlayingModel) bool
	}{
		{"play/pause", " ", opPlayPause, func(np *nowPlayingModel) uint64 { return np.playPauseFlip.seq }, func(np *nowPlayingModel) bool { return np.playing }},
		{"shuffle", "r", opShuffle, func(np *nowPlayingModel) uint64 { return np.shuffleFlip.seq }, func(np *nowPlayingModel) bool { return np.shuffling }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newIntentTestModel()
			m.nowPlaying.hasTrack = true

			m, _ = pressKeys(t, m, keyMsg(tc.key))
			first := tc.pending(m.nowPlaying)
			m, _ = pressKeys(t, m, keyMsg(tc.key))
			second := tc.pending(m.nowPlaying)
			want := tc.state(m.nowPlaying) // the state after the second press

			updated, _ := m.Update(playbackResultMsg{op: tc.op, err: errTest, flip: first})
			m = updated.(Model)
			if got := tc.state(m.nowPlaying); got != want {
				t.Errorf("state = %v after the first press's failure, want the second press's %v", got, want)
			}
			if got := tc.pending(m.nowPlaying); got != second {
				t.Errorf("pending flip = %d, want the second press's %d", got, second)
			}

			updated, _ = m.Update(playbackResultMsg{op: tc.op, err: errTest, flip: second})
			m = updated.(Model)
			if got := tc.state(m.nowPlaying); got == want || tc.pending(m.nowPlaying) != 0 {
				t.Errorf("second press's failure: state=%v pending=%d, want reverted and settled", got, tc.pending(m.nowPlaying))
			}
		})
	}
}
