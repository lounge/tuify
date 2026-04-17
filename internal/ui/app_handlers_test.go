package ui

import (
	"testing"

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

// handleResize: each view in the stack gets a height budget equal to the
// terminal height minus the now-playing bar, and minus the breadcrumb row
// ONLY when the view declares a non-empty breadcrumb. A height miscalculation
// here compounds into pagination math inside bubbles/list and has caused
// render panics before.

type heightCaptureView struct {
	breadcrumb string
	gotWidth   int
	gotHeight  int
}

func (v *heightCaptureView) Update(msg tea.Msg) tea.Cmd        { return nil }
func (v *heightCaptureView) View() string                      { return "" }
func (v *heightCaptureView) SetSize(width, height int)         { v.gotWidth = width; v.gotHeight = height }
func (v *heightCaptureView) Breadcrumb() string                { return v.breadcrumb }

func TestHandleResize_SubtractsBreadcrumbOnlyWhenPresent(t *testing.T) {
	withCrumb := &heightCaptureView{breadcrumb: "Home > Playlists"}
	noCrumb := &heightCaptureView{breadcrumb: ""}
	m := Model{
		nowPlaying: &nowPlayingModel{},
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
