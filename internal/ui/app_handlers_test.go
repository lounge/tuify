package ui

import (
	"context"
	"testing"

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

func (v *heightCaptureView) Update(msg tea.Msg) tea.Cmd { return nil }
func (v *heightCaptureView) View() string               { return "" }
func (v *heightCaptureView) SetSize(width, height int)  { v.gotWidth = width; v.gotHeight = height }
func (v *heightCaptureView) Breadcrumb() string         { return v.breadcrumb }

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
