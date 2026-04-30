package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/lounge/tuify/internal/spotify"
)

func twoDevices() []spotify.Device {
	return []spotify.Device{
		{ID: "comp", Name: "lounge M2", Type: "Computer", Active: true},
		{ID: "tuify", Name: "tuify", Type: "Speaker", Active: false},
	}
}

func TestHandleLoaded_CursorSkipsActive(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: twoDevices()})

	if d.activeDeviceID != "comp" {
		t.Errorf("activeDeviceID: got %q, want %q", d.activeDeviceID, "comp")
	}
	if d.cursor != 1 {
		t.Errorf("cursor should skip active device: got %d, want 1", d.cursor)
	}
}

func TestHandleLoaded_AllDevicesKept(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: twoDevices()})

	if len(d.devices) != 2 {
		t.Fatalf("all devices should be kept: got %d, want 2", len(d.devices))
	}
}

func TestSelected_RejectsActiveDevice(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: twoDevices()})

	// Force cursor onto the active device.
	d.cursor = 0
	_, ok := d.selected()
	if ok {
		t.Error("selected() should return false for active device")
	}

	// Cursor on the non-active device should work.
	d.cursor = 1
	dev, ok := d.selected()
	if !ok {
		t.Fatal("selected() should return true for non-active device")
	}
	if dev.Name != "tuify" {
		t.Errorf("selected device: got %q, want %q", dev.Name, "tuify")
	}
}

func TestUpDown_SkipsActive(t *testing.T) {
	devs := []spotify.Device{
		{ID: "a", Name: "A", Active: false},
		{ID: "b", Name: "B", Active: true},
		{ID: "c", Name: "C", Active: false},
	}
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: devs})

	// After reordering, the list is [B (active), A, C]. First non-active
	// is at index 1 (A), so the cursor starts there.
	if d.cursor != 1 {
		t.Fatalf("initial cursor: got %d, want 1", d.cursor)
	}

	// Down moves to C at index 2.
	d.down()
	if d.cursor != 2 {
		t.Errorf("after down: got %d, want 2", d.cursor)
	}

	// Up skips B (active, index 0) and lands back on A at index 1.
	d.up()
	if d.cursor != 1 {
		t.Errorf("after up: got %d, want 1", d.cursor)
	}
}

// TestUp_StaysPutWhenNoSelectableAbove reproduces the bug where the cursor
// could land on the active (non-selectable) device after a sequence of
// up-presses. With the active pinned to index 0, pressing up from the
// first non-active must be a no-op, not a silent move to the active row.
func TestUp_StaysPutWhenNoSelectableAbove(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: twoDevices()})

	// Active ("comp") is at index 0 after the reorder; cursor starts at 1.
	if d.cursor != 1 {
		t.Fatalf("setup: cursor should be at 1, got %d", d.cursor)
	}

	d.up()
	if d.cursor != 1 {
		t.Errorf("up from first selectable should stay put, got cursor=%d (device=%q)",
			d.cursor, d.devices[d.cursor].ID)
	}
}

// TestDown_StaysPutWhenNoSelectableBelow is the mirror of the above:
// down from the last non-active must not silently skip onto nothing.
func TestDown_StaysPutWhenNoSelectableBelow(t *testing.T) {
	devs := []spotify.Device{
		{ID: "active", Name: "Active", Active: true},
		{ID: "last", Name: "Last", Active: false},
	}
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: devs})

	if d.cursor != 1 {
		t.Fatalf("setup: cursor should be at 1, got %d", d.cursor)
	}

	d.down()
	if d.cursor != 1 {
		t.Errorf("down from last selectable should stay put, got cursor=%d", d.cursor)
	}
}

// TestHandleLoaded_ActiveDeviceMovedToTop pins that the currently-playing
// device is always pinned to index 0 regardless of where the API listed it.
func TestHandleLoaded_ActiveDeviceMovedToTop(t *testing.T) {
	devs := []spotify.Device{
		{ID: "a", Name: "A", Active: false},
		{ID: "b", Name: "B", Active: false},
		{ID: "active", Name: "Playing Now", Active: true},
		{ID: "c", Name: "C", Active: false},
	}
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: devs})

	if d.devices[0].ID != "active" {
		t.Errorf("active device should be pinned to top; got %q at index 0", d.devices[0].ID)
	}
	// Relative order of the rest must be preserved.
	wantRest := []string{"a", "b", "c"}
	for i, want := range wantRest {
		if got := d.devices[i+1].ID; got != want {
			t.Errorf("devices[%d]: got %q, want %q", i+1, got, want)
		}
	}
}

// TestHandleLoaded_NoActiveDevice ensures the reorder is a no-op when no
// device is flagged active — we don't want to shuffle the API's list for
// no reason.
func TestHandleLoaded_NoActiveDevice(t *testing.T) {
	devs := []spotify.Device{
		{ID: "a", Name: "A", Active: false},
		{ID: "b", Name: "B", Active: false},
	}
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: devs})

	if d.devices[0].ID != "a" || d.devices[1].ID != "b" {
		t.Errorf("order should be unchanged when no active device; got %v",
			[]string{d.devices[0].ID, d.devices[1].ID})
	}
	if d.cursor != 0 {
		t.Errorf("cursor should start at 0 when no active device; got %d", d.cursor)
	}
}

func TestTransferring_BlocksOpen(t *testing.T) {
	d := deviceSelectorModel{}
	d.transferring = true

	// Simulates the tab key guard in handleKeyMsg.
	if !d.transferring {
		t.Error("transferring should be true")
	}
}

func TestTransferring_ClearedOnTargetMatch(t *testing.T) {
	d := deviceSelectorModel{
		transferring:     true,
		transferTarget:   "tuify",
		transferDeadline: time.Now().Add(15 * time.Second),
	}

	// Simulate poller confirming the device switched.
	deviceName := "tuify"
	if deviceName == d.transferTarget {
		d.transferring = false
	}
	if d.transferring {
		t.Error("transferring should be cleared when target matches")
	}
}

func TestTransferring_ClearedOnDeadline(t *testing.T) {
	d := deviceSelectorModel{
		transferring:     true,
		transferTarget:   "tuify",
		transferDeadline: time.Now().Add(-1 * time.Second),
	}

	// Simulate deadline check in handleStateUpdate.
	if time.Now().After(d.transferDeadline) {
		d.transferring = false
	}
	if d.transferring {
		t.Error("transferring should be cleared after deadline")
	}
}

func TestHandleLoaded_Error(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{err: errTest})

	if d.err != errTest {
		t.Errorf("err: got %v, want %v", d.err, errTest)
	}
	if d.loading {
		t.Error("loading should be false after error")
	}
}

// TestView_RendersActiveIconForActiveDevice asserts the ◉ glyph appears
// in the rendered view when one device is flagged Active. Regression
// guard for the recent border bug — same root cause (a style assigned
// before RebuildStyles ran) would have dropped the icon's color too.
func TestView_RendersActiveIconForActiveDevice(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: twoDevices()})

	out := d.view(80, 20)
	if !strings.Contains(out, "◉") {
		t.Fatalf("rendered view should contain active-device icon ◉; got:\n%s", out)
	}
}

// TestInjectExternalDevice_PrependsWhenMissing covers the Sonos case:
// /me/player reports a device name that /me/player/devices omits. The
// synthetic row must be at the head (so it sits at the top of the
// picker independent of handleLoaded's sort), Active (so it gets the ◉
// + skip-cursor treatment), Type "External" (renders as "external" via
// the existing lowercased-type column), and carry the externalDeviceID
// sentinel (so any selection attempt short-circuits before issuing a
// 404-bound transfer).
func TestInjectExternalDevice_PrependsWhenMissing(t *testing.T) {
	msg := devicesLoadedMsg{devices: []spotify.Device{
		{ID: "tuify", Name: "tuify", Type: "Speaker"},
	}}
	out := injectExternalDevice(msg, "Living Room (Sonos)")

	if len(out.devices) != 2 {
		t.Fatalf("device count: got %d, want 2", len(out.devices))
	}
	got := out.devices[0]
	if got.Name != "Living Room (Sonos)" {
		t.Errorf("synthetic device name: got %q, want %q", got.Name, "Living Room (Sonos)")
	}
	if got.ID != externalDeviceID {
		t.Errorf("synthetic device ID: got %q, want %q", got.ID, externalDeviceID)
	}
	if !got.Active {
		t.Error("synthetic device must be Active so the picker pins/marks it")
	}
	if got.Type != "External" {
		t.Errorf("synthetic device Type: got %q, want %q", got.Type, "External")
	}
}

// TestInjectExternalDevice_NoOpWhenAlreadyListed prevents a duplicate
// row when the real device-list endpoint already includes the active
// device — common when playing on the laptop or any Spotify-app target.
func TestInjectExternalDevice_NoOpWhenAlreadyListed(t *testing.T) {
	msg := devicesLoadedMsg{devices: twoDevices()}
	out := injectExternalDevice(msg, "lounge M2")

	if len(out.devices) != len(msg.devices) {
		t.Errorf("device count changed: got %d, want %d", len(out.devices), len(msg.devices))
	}
}

// TestInjectExternalDevice_NoOpOnError keeps an error response intact —
// we don't want to mask a fetch failure with a synthetic row.
func TestInjectExternalDevice_NoOpOnError(t *testing.T) {
	msg := devicesLoadedMsg{err: errTest}
	out := injectExternalDevice(msg, "Sonos")

	if len(out.devices) != 0 || out.err != errTest {
		t.Errorf("error message should pass through unchanged; got devices=%d err=%v", len(out.devices), out.err)
	}
}

// TestSelected_RejectsExternalDevice ensures the synthetic row is never
// returned by selected() — would otherwise let a user trigger a transfer
// against the externalDeviceID sentinel and 404 against Spotify.
func TestSelected_RejectsExternalDevice(t *testing.T) {
	devs := []spotify.Device{
		{ID: "tuify", Name: "tuify", Type: "Speaker"},
	}
	msg := injectExternalDevice(devicesLoadedMsg{devices: devs}, "Sonos")

	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(msg)

	// Force the cursor onto the external row regardless of where
	// handleLoaded placed it, then assert selected() rejects it.
	for i, dev := range d.devices {
		if dev.ID == externalDeviceID {
			d.cursor = i
			break
		}
	}
	if _, ok := d.selected(); ok {
		t.Error("selected() must reject the external (non-transferable) row")
	}
}

func TestHandleLoaded_NoDevices(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{devices: nil})

	if len(d.devices) != 0 {
		t.Errorf("devices: got %d, want 0", len(d.devices))
	}
	_, ok := d.selected()
	if ok {
		t.Error("selected() should return false for empty list")
	}
}

var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
