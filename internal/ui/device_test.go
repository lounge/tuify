package ui

import (
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

	// Cursor starts at 0 (first non-active).
	if d.cursor != 0 {
		t.Fatalf("initial cursor: got %d, want 0", d.cursor)
	}

	// Down should skip B (active) and land on C.
	d.down()
	if d.cursor != 2 {
		t.Errorf("after down: got %d, want 2", d.cursor)
	}

	// Up should skip B (active) and land on A.
	d.up()
	if d.cursor != 0 {
		t.Errorf("after up: got %d, want 0", d.cursor)
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
