package ui

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// Seek sequence suppression: when the user rapid-fires seek keys, each press
// bumps m.seekSeq and schedules a delayed seekFireMsg. Only the most recent
// message should fire — otherwise a slow older seek rewinds the position
// after the newer one already moved forward.

func TestHandleSeekFire_StaleSequenceIsDropped(t *testing.T) {
	m := Model{
		nowPlaying: &nowPlayingModel{},
		client:     &spotify.Client{},
		seekSeq:    5, // current "latest" seek
	}

	_, cmd := m.handleSeekFire(seekFireMsg{seq: 3, posMs: 10000})
	if cmd != nil {
		t.Error("stale seekFireMsg should return nil cmd — a newer seek already superseded it")
	}
}

func TestHandleSeekFire_CurrentSequenceFires(t *testing.T) {
	m := Model{
		nowPlaying: &nowPlayingModel{},
		client:     &spotify.Client{},
		seekSeq:    7,
	}

	_, cmd := m.handleSeekFire(seekFireMsg{seq: 7, posMs: 42000})
	if cmd == nil {
		t.Fatal("matching seq should return a seek command")
	}
}

// A seek position is computed for the track playing when the key was
// pressed. If the track changes during the debounce (it ended, or the
// user skipped), applying that position to the new track would jump into
// it.
func TestHandleSeekFire_TrackChangedIsDropped(t *testing.T) {
	np := &nowPlayingModel{trackURI: "spotify:track:new", seekPending: true}
	m := Model{nowPlaying: np, client: &spotify.Client{}, seekSeq: 7}

	_, cmd := m.handleSeekFire(seekFireMsg{seq: 7, posMs: 42000, trackURI: "spotify:track:old"})
	if cmd != nil {
		t.Error("seek scheduled for another track should not fire")
	}
	if np.seekPending {
		t.Error("seekPending must be released: no seek reply will clear it, and polls would never update progress")
	}
}

func TestSeekRelative_StampsCurrentTrack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &Model{
			nowPlaying: &nowPlayingModel{trackURI: "spotify:track:a", hasTrack: true, progressMs: 10000, durationMs: 100000},
			client:     &spotify.Client{},
		}
		fire, ok := m.seekRelative(5000)().(seekFireMsg)
		if !ok {
			t.Fatal("seekRelative did not produce a seekFireMsg")
		}
		if fire.trackURI != "spotify:track:a" || fire.posMs != 15000 || fire.seq != m.seekSeq {
			t.Errorf("seekFireMsg = %+v, want track spotify:track:a at 15000ms with seq %d", fire, m.seekSeq)
		}
	})
}

// handlePlaybackResult reverts an optimistic flip only when the failed
// command is the one that made it. Space (pause, pending) followed by a
// failing Next must leave the pause in place: it succeeded, and flipping
// it back would show "playing" until the next poll corrected it.
func TestHandlePlaybackResult_FailureRevertsOnlyItsOwnFlip(t *testing.T) {
	tests := []struct {
		name               string
		op                 playbackOp
		wantPlaying        bool
		wantPlayPending    bool
		wantShuffling      bool
		wantShufflePending bool
	}{
		{"next fails", opPlayback, false, true, true, true},
		{"seek fails", opSeek, false, true, true, true},
		{"pause fails", opPlayPause, true, false, true, true},
		{"shuffle fails", opShuffle, false, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModelWithClient("")
			// The user paused and turned shuffle on; neither reply is in yet.
			m.nowPlaying.playing, m.nowPlaying.playPauseFlip.seq = false, m.nowPlaying.beginFlip()
			m.nowPlaying.shuffling, m.nowPlaying.shuffleFlip.seq = true, m.nowPlaying.beginFlip()
			var flip uint64
			switch tt.op {
			case opPlayPause:
				flip = m.nowPlaying.playPauseFlip.seq
			case opShuffle:
				flip = m.nowPlaying.shuffleFlip.seq
			case opPlayback, opSeek:
			}

			updated, _ := m.handlePlaybackResult(playbackResultMsg{op: tt.op, err: errTest, flip: flip})
			np := updated.(Model).nowPlaying
			if np.playing != tt.wantPlaying || (np.playPauseFlip.seq != 0) != tt.wantPlayPending {
				t.Errorf("playing=%v playPauseFlip.seq=%v, want %v/%v",
					np.playing, np.playPauseFlip.seq, tt.wantPlaying, tt.wantPlayPending)
			}
			if np.shuffling != tt.wantShuffling || (np.shuffleFlip.seq != 0) != tt.wantShufflePending {
				t.Errorf("shuffling=%v shuffleFlip.seq=%v, want %v/%v",
					np.shuffling, np.shuffleFlip.seq, tt.wantShuffling, tt.wantShufflePending)
			}
		})
	}
}

// A command Spotify accepted starts the settle window of its own flip, so
// a poll that keeps contradicting it is eventually believed. A reply for a
// flip the user has since replaced with another press leaves the newer
// flip waiting on its own reply.
func TestHandlePlaybackResult_SuccessStartsOwnSettleWindow(t *testing.T) {
	m := newTestModelWithClient("")
	first := m.nowPlaying.beginFlip()
	m.nowPlaying.playing, m.nowPlaying.playPauseFlip.seq = false, m.nowPlaying.beginFlip()
	m.nowPlaying.shuffling, m.nowPlaying.shuffleFlip.seq = true, m.nowPlaying.beginFlip()

	updated, _ := m.handlePlaybackResult(playbackResultMsg{op: opPlayPause, flip: first})
	m = updated.(Model)
	if !m.nowPlaying.playPauseFlip.settleBy.IsZero() {
		t.Error("a superseded flip's reply started the pending flip's settle window")
	}

	before := time.Now()
	updated, _ = m.handlePlaybackResult(playbackResultMsg{op: opPlayPause, flip: m.nowPlaying.playPauseFlip.seq})
	m = updated.(Model)
	if got := m.nowPlaying.playPauseFlip.settleBy; got.Before(before.Add(flipSettleWindow)) {
		t.Errorf("playPauseFlip.settleBy = %v, want at least %v after the reply", got.Sub(before), flipSettleWindow)
	}
	if !m.nowPlaying.shuffleFlip.settleBy.IsZero() {
		t.Error("play/pause reply started the shuffle flip's settle window")
	}

	// A new press is a new command in flight: its window starts over.
	m, _ = pressKeys(t, m, runeKey(" "))
	if !m.nowPlaying.playPauseFlip.settleBy.IsZero() {
		t.Error("a new press kept the previous flip's settle deadline")
	}
}

// handleResize: each view in the stack gets a height budget equal to the
// terminal height minus the now-playing bar, and minus the breadcrumb row
// ONLY when the view declares a non-empty breadcrumb. A height miscalculation
// here compounds into pagination math inside bubbles/list and has caused
// render panics before.

type heightCaptureView struct {
	crumb     string
	gotWidth  int
	gotHeight int
}

func (v *heightCaptureView) Update(msg tea.Msg) tea.Cmd { return nil }
func (v *heightCaptureView) View() string               { return "" }
func (v *heightCaptureView) SetSize(width, height int)  { v.gotWidth = width; v.gotHeight = height }
func (v *heightCaptureView) breadcrumb() string         { return v.crumb }

func TestHandleResize_SubtractsBreadcrumbOnlyWhenPresent(t *testing.T) {
	withCrumb := &heightCaptureView{crumb: "Home > Playlists"}
	noCrumb := &heightCaptureView{crumb: ""}
	m := Model{
		nowPlaying: &nowPlayingModel{},
		visualizer: newVisualizerModel(context.Background(), nil),
		viewStack:  []view{noCrumb, withCrumb},
	}

	m.handleResize(tea.WindowSizeMsg{Width: 100, Height: 40})

	expectedNoCrumb := 40 - nowPlayingHeight
	expectedWithCrumb := 40 - nowPlayingHeight - breadcrumbHeight
	if noCrumb.gotHeight != expectedNoCrumb {
		t.Errorf("no-breadcrumb view: got height %d, want %d", noCrumb.gotHeight, expectedNoCrumb)
	}
	if withCrumb.gotHeight != expectedWithCrumb {
		t.Errorf("breadcrumb view: got height %d, want %d", withCrumb.gotHeight, expectedWithCrumb)
	}
	if noCrumb.gotWidth != 100 || withCrumb.gotWidth != 100 {
		t.Error("both views should receive the full terminal width")
	}
}

// Double-click bookkeeping must be per-zone-id: rapidly clicking on two
// different items should never fire Enter, even if the second click
// happens within doubleClickWindow. Only matching ids close the pair.
func TestRegisterClick_DifferentIdsDontTriggerActivate(t *testing.T) {
	m := newTestModelWithClient("")

	handled, m1, cmd := m.registerClick("id-a")
	if !handled {
		t.Fatal("first click should be handled")
	}
	if cmd != nil {
		t.Errorf("first click should not fire Enter: got cmd %v", cmd)
	}

	// Second click on a DIFFERENT id, well within the doubleClickWindow.
	m2 := m1.(Model)
	handled, _, cmd = m2.registerClick("id-b")
	if !handled {
		t.Fatal("second click should be handled")
	}
	if cmd != nil {
		t.Errorf("click on different id must not fire Enter: got cmd %v", cmd)
	}
}

// handleMouseClick on a list-backed view that contains only non-URI
// items (e.g. a lone loading status row) should report the click as
// unhandled so the caller can decide what to do — NOT silently absorb it.
func TestHandleMouseClick_ListWithNoURIItems_ReportsUnhandled(t *testing.T) {
	tv := newTrackView(context.Background(), nil, "pid", "Test Playlist", 80, 20, false)
	tv.items = []list.Item{statusItem{text: "Loading…"}}
	tv.list.SetItems(tv.items)
	m := newTestModelWithClient("")
	m.viewStack = []view{tv}

	click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 0}
	handled, _, _ := m.handleMouseClick(click)
	if handled {
		t.Error("click on a list with no uriItems should report unhandled")
	}
}

// Intent dispatch contract: views emit intents (see app_intents.go) and the
// shell interprets them by constructing the target view or dispatching the
// corresponding command. These tests pin that contract — adding a new intent
// without wiring Model.Update to handle it will fail them.

// While an overlay or mode hides the list, wheel events must not move its
// cursor and clicks must not resolve against the stale zones of the last
// list frame.
func TestHandleMouse_IgnoredWhileListHidden(t *testing.T) {
	cases := map[string]func(*Model){
		"help":       func(m *Model) { m.showHelp = true },
		"devices":    func(m *Model) { m.showDeviceSelector = true },
		"mini mode":  func(m *Model) { m.miniMode = true },
		"visualizer": func(m *Model) { m.visualizer.active = true },
	}
	for name, hide := range cases {
		t.Run(name, func(t *testing.T) {
			tv := newTrackView(context.Background(), nil, "pid", "Test Playlist", 80, 20, false)
			tv.items = []list.Item{
				trackItem{uri: "spotify:track:a", name: "A"},
				trackItem{uri: "spotify:track:b", name: "B"},
			}
			tv.list.SetItems(tv.items)
			m := newIntentTestModel()
			m.viewStack = []view{tv}
			hide(&m)

			wheel := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
			handled, _, cmd := m.handleMouse(wheel)
			if !handled || cmd != nil {
				t.Errorf("wheel: handled=%v cmd=%v, want consumed with no cmd", handled, cmd)
			}
			if got := tv.list.Index(); got != 0 {
				t.Errorf("wheel moved the hidden list cursor to %d", got)
			}

			click := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			_, model, cmd := m.handleMouse(click)
			if cmd != nil {
				t.Errorf("click on hidden list returned a cmd: %v", cmd)
			}
			if got := model.(Model).lastClickID; got != "" {
				t.Errorf("click registered on hidden row %q", got)
			}
		})
	}
}
