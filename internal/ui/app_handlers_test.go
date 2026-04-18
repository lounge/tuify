package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// renderTrackLine uses display-cell widths so wide runes (CJK, emoji)
// that count as 1 Unicode point but 2 cells don't slip past the budget
// and wrap the now-playing bar. A wrap would shift zone coordinates in
// the list above and make mouse clicks land on the wrong row.
func TestRenderTrackLine_WideRunesStaySingleLine(t *testing.T) {
	np := &nowPlayingModel{
		width:    40,
		hasTrack: true,
		track:    "あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほ", // ~60 cells of wide runes
		artist:   "まみむめも",
		playing:  true,
	}
	line := np.renderTrackLine()
	if strings.Contains(line, "\n") {
		t.Errorf("renderTrackLine produced multi-line output: %q", line)
	}
	// The rendered line should fit within the width budget once styling
	// is applied. lipgloss.Width ignores ANSI codes, so it's the cell count.
	if w := lipgloss.Width(line); w > np.width-nowPlayingPadding {
		t.Errorf("line width %d exceeds budget %d", w, np.width-nowPlayingPadding)
	}
}

// handleMouseClick on a list-backed view that contains only non-URI
// items (e.g. a lone loading status row) should report the click as
// unhandled so the caller can decide what to do — NOT silently absorb it.
func TestHandleMouseClick_ListWithNoURIItems_ReportsUnhandled(t *testing.T) {
	tv := newTrackView(nil, "pid", "Test Playlist", 80, 20, false)
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
