package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// The waitFor* Cmds park on bootstrap channels that usually never fire.
// They must return once the root context is cancelled, and must not re-arm
// into a busy loop when their channel is closed.
func TestWaitCmds_ExitOnShutdownAndClosedChannel(t *testing.T) {
	cmds := []struct {
		name string
		cmd  func(Model) tea.Cmd
	}{
		{"librespotInactive", Model.waitForLibrespotInactive},
		{"tokenSaveErr", Model.waitForTokenSaveErr},
		{"tokenRevoked", Model.waitForTokenRevoked},
	}
	for _, tc := range cmds {
		t.Run(tc.name+"/shutdown", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			m := Model{
				rootCtx:             ctx,
				librespotInactiveCh: make(chan struct{}),
				tokenSaveErrCh:      make(chan error),
				tokenRevokedCh:      make(chan struct{}),
			}
			done := runCmd(tc.cmd(m))
			cancel()
			assertReturnsNil(t, done)
		})
		t.Run(tc.name+"/closed", func(t *testing.T) {
			libCh, saveCh, revCh := make(chan struct{}), make(chan error), make(chan struct{})
			close(libCh)
			close(saveCh)
			close(revCh)
			m := Model{
				rootCtx:             t.Context(),
				librespotInactiveCh: libCh,
				tokenSaveErrCh:      saveCh,
				tokenRevokedCh:      revCh,
			}
			assertReturnsNil(t, runCmd(tc.cmd(m)))
		})
	}
}

func runCmd(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	return done
}

func assertReturnsNil(t *testing.T, done <-chan tea.Msg) {
	t.Helper()
	select {
	case msg := <-done:
		if msg != nil {
			t.Errorf("got %T, want nil", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cmd did not return")
	}
}

// transferDeviceMsg: switching back to the preferred device must CLEAR the
// override flag; switching to anything else must SET it. The override flag
// gates librespot reconnect behavior, so inverting this logic would cause
// tuify to fight the user over device selection.

func newTestModelWithClient(preferred string) Model {
	client := &spotify.Client{PreferredDevice: preferred}
	np := newNowPlaying(client)
	return Model{
		nowPlaying: np,
		client:     client,
		viewStack:  []view{newHomeView(0, 0)},
	}
}

func TestUpdate_TransferDeviceMsg_PreferredClearsOverride(t *testing.T) {
	m := newTestModelWithClient("tuify")
	m.nowPlaying.setDeviceOverride(true, "test setup: pretend user overrode")
	if !m.nowPlaying.deviceOverridden {
		t.Fatal("test setup failed: override should be set before the msg")
	}

	updated, _ := m.Update(transferDeviceMsg{deviceName: "tuify"})
	after := updated.(Model)

	if after.nowPlaying.deviceOverridden {
		t.Error("transferring to the preferred device should clear deviceOverridden")
	}
	if after.nowPlaying.deviceName != "tuify" {
		t.Errorf("deviceName should be updated to %q, got %q", "tuify", after.nowPlaying.deviceName)
	}
}

func TestUpdate_TransferDeviceMsg_NonPreferredSetsOverride(t *testing.T) {
	m := newTestModelWithClient("tuify")

	updated, _ := m.Update(transferDeviceMsg{deviceName: "Living Room Speaker"})
	after := updated.(Model)

	if !after.nowPlaying.deviceOverridden {
		t.Error("transferring to a non-preferred device should set deviceOverridden so librespot reconnect respects the user's choice")
	}
	if after.nowPlaying.deviceName != "Living Room Speaker" {
		t.Errorf("deviceName should track the new device, got %q", after.nowPlaying.deviceName)
	}
}

// newIntentTestModel constructs a Model adequate for intent-dispatch tests:
// nowPlaying/visualizer defaulted, rootCtx set so view constructors don't
// receive nil, and an empty viewStack caller seeds with a homeView.
func newIntentTestModel() Model {
	m := newTestModelWithClient("")
	m.rootCtx = context.Background()
	m.visualizer = newVisualizerModel(false)
	return m
}

func TestUpdate_OpenSearchIntent_PushesSearchView(t *testing.T) {
	m := newIntentTestModel()
	before := len(m.viewStack)

	updated, _ := m.Update(openSearchIntent{})
	after := updated.(Model)

	if got := len(after.viewStack); got != before+1 {
		t.Fatalf("viewStack grew by %d, want 1", got-before)
	}
	if _, ok := after.currentView().(*searchView); !ok {
		t.Errorf("top of viewStack = %T, want *searchView", after.currentView())
	}
}

func TestUpdate_OpenTracksIntent_CarriesPlaylistInfo(t *testing.T) {
	m := newIntentTestModel()

	updated, cmd := m.Update(openTracksIntent{playlistID: "pid-123", playlistName: "Roadtrip"})
	after := updated.(Model)

	tv, ok := after.currentView().(*trackView)
	if !ok {
		t.Fatalf("top of viewStack = %T, want *trackView", after.currentView())
	}
	if tv.playlistID != "pid-123" {
		t.Errorf("playlistID: got %q, want %q", tv.playlistID, "pid-123")
	}
	if tv.playlistName != "Roadtrip" {
		t.Errorf("playlistName: got %q, want %q", tv.playlistName, "Roadtrip")
	}
	if cmd == nil {
		t.Error("expected Init cmd from the new trackView, got nil")
	}
}

func TestUpdate_OpenEpisodesIntent_CarriesShowInfo(t *testing.T) {
	m := newIntentTestModel()

	updated, cmd := m.Update(openEpisodesIntent{showID: "sid-456", showName: "Daily News"})
	after := updated.(Model)

	ev, ok := after.currentView().(*episodeView)
	if !ok {
		t.Fatalf("top of viewStack = %T, want *episodeView", after.currentView())
	}
	if ev.showID != "sid-456" {
		t.Errorf("showID: got %q, want %q", ev.showID, "sid-456")
	}
	if ev.showName != "Daily News" {
		t.Errorf("showName: got %q, want %q", ev.showName, "Daily News")
	}
	if cmd == nil {
		t.Error("expected Init cmd from the new episodeView, got nil")
	}
}

// playItemIntent and playQueueIntent get dispatched through m.playItem /
// m.playQueue, which build a device-resolving Cmd. The cmd hits the Spotify
// API when executed, so the test asserts only that a Cmd is returned —
// proving the intent was recognised and dispatched.
func TestUpdate_PlayItemIntent_ReturnsCmd(t *testing.T) {
	m := newIntentTestModel()
	before := len(m.viewStack)

	updated, cmd := m.Update(playItemIntent{itemURI: "spotify:track:abc", contextURI: "spotify:playlist:xyz"})
	after := updated.(Model)

	if cmd == nil {
		t.Error("playItemIntent should dispatch a playback cmd, got nil")
	}
	if got := len(after.viewStack); got != before {
		t.Errorf("viewStack changed by %d; play intents should not navigate", got-before)
	}
}

func TestUpdate_PlayQueueIntent_ReturnsCmd(t *testing.T) {
	m := newIntentTestModel()

	_, cmd := m.Update(playQueueIntent{uris: []string{"spotify:track:a", "spotify:track:b"}})
	if cmd == nil {
		t.Error("playQueueIntent should dispatch a playback cmd, got nil")
	}
}
