package ui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// fakePlayer answers the player calls transferDeviceCmd makes, in memory
// so the post-transfer settle delay runs on synctest's clock, and records
// each call as "METHOD /path body".
type fakePlayer struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakePlayer) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, strings.TrimSpace(req.Method+" "+req.URL.Path+" "+string(body)))
	f.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func (f *fakePlayer) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// A transfer with play=true starts playback on the target, so a paused
// session must transfer with play=false. The exception is the seek that
// restores progress when leaving the librespot device: it needs an active
// target, so that path forces play and then re-pauses.
func TestTransferDeviceCmd_PreservesPausedState(t *testing.T) {
	tuify := spotify.Device{ID: "tuify-id", Name: "tuify"}
	phone := spotify.Device{ID: "phone", Name: "Phone"}
	const transfer, seek, pause = "PUT /v1/me/player ", "PUT /v1/me/player/seek", "PUT /v1/me/player/pause"
	tests := []struct {
		name       string
		dev        spotify.Device
		current    string
		progressMs int
		playing    bool
		wantPlay   bool     // the play flag sent with the transfer
		wantCalls  []string // path prefixes, in order
	}{
		{"paused, to the preferred device", tuify, "phone", 30000, false, false, []string{transfer}},
		{"playing, to the preferred device", tuify, "phone", 30000, true, true, []string{transfer}},
		{"paused, away from the preferred device at 0ms", phone, "tuify-id", 0, false, false, []string{transfer}},
		{"paused, away from the preferred device mid-track", phone, "tuify-id", 30000, false, true, []string{transfer, seek, pause}},
		{"playing, away from the preferred device mid-track", phone, "tuify-id", 30000, true, true, []string{transfer, seek}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := &fakePlayer{}
				client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))

				msg := transferDeviceCmd(t.Context(), client, tc.dev, tc.current, tc.progressMs, tc.playing)()
				if tm, ok := msg.(transferDeviceMsg); !ok || tm.err != nil {
					t.Fatalf("transfer result = %#v", msg)
				}

				calls := fake.snapshot()
				if len(calls) != len(tc.wantCalls) {
					t.Fatalf("calls = %q, want %d: %q", calls, len(tc.wantCalls), tc.wantCalls)
				}
				for i, want := range tc.wantCalls {
					if !strings.HasPrefix(calls[i], want) {
						t.Errorf("call %d = %q, want prefix %q", i, calls[i], want)
					}
				}
				if gotPlay := strings.Contains(calls[0], `"play":true`); gotPlay != tc.wantPlay {
					t.Errorf("transfer sent play=%v, want %v: %s", gotPlay, tc.wantPlay, calls[0])
				}
			})
		})
	}
}

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

// While a transfer is in flight, Tab must not open the device selector.
func TestUpdate_TabIgnoredWhileTransferring(t *testing.T) {
	m := newIntentTestModel()
	m.deviceSelector.transferring = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	after := updated.(Model)

	if after.showDeviceSelector {
		t.Error("Tab opened the device selector during a transfer")
	}
	if cmd != nil {
		t.Errorf("Tab during a transfer returned a cmd (device fetch?): %v", cmd)
	}
}

// Tab, Esc, Tab leaves the first fetch in flight. Its late reply must not
// re-run handleLoaded over the second fetch's list and reset the cursor
// the user has since moved.
func TestUpdate_ReopenedSelectorIgnoresEarlierFetch(t *testing.T) {
	devs := []spotify.Device{
		{ID: "active", Name: "Active", Active: true},
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}
	m := newIntentTestModel()
	for _, key := range []string{"tab", "esc", "tab"} {
		updated, _ := m.Update(keyMsg(key))
		m = updated.(Model)
	}
	if !m.showDeviceSelector || !m.deviceSelector.loading {
		t.Fatalf("setup: open=%v loading=%v, want the selector open and loading", m.showDeviceSelector, m.deviceSelector.loading)
	}

	updated, _ := m.Update(devicesLoadedMsg{seq: 2, devices: devs})
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("down"))
	m = updated.(Model)
	if m.deviceSelector.cursor != 2 {
		t.Fatalf("setup: cursor = %d after down, want 2", m.deviceSelector.cursor)
	}

	updated, _ = m.Update(devicesLoadedMsg{seq: 1, devices: devs})
	m = updated.(Model)

	if m.deviceSelector.cursor != 2 {
		t.Errorf("cursor = %d after the first fetch's late reply, want 2 (left where the user put it)", m.deviceSelector.cursor)
	}
}

// The transfer lock clears once a player-state poll reports the target
// device, or once its deadline passes; until then it holds.
func TestUpdate_PlayerStateClearsTransferLock(t *testing.T) {
	tests := []struct {
		name     string
		device   string
		deadline time.Duration
		want     bool // still transferring afterwards
	}{
		{"target confirmed", "tuify", time.Minute, false},
		{"deadline passed", "Phone", -time.Second, false},
		{"still waiting", "Phone", time.Minute, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newIntentTestModel()
			m.deviceSelector.transferring = true
			m.deviceSelector.transferTarget = "tuify"
			m.deviceSelector.transferDeadline = time.Now().Add(tc.deadline)

			updated, _ := m.Update(playerStateMsg{state: &spotify.PlayerState{
				TrackURI: "spotify:track:1", DurationMs: 1000, DeviceName: tc.device,
			}})
			if got := updated.(Model).deviceSelector.transferring; got != tc.want {
				t.Errorf("transferring = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleLoaded_Error(t *testing.T) {
	d := deviceSelectorModel{}
	d.open()
	d.handleLoaded(devicesLoadedMsg{err: errTest})

	if !errors.Is(d.err, errTest) {
		t.Errorf("err: got %v, want %v", d.err, errTest)
	}
	if d.loading {
		t.Error("loading should be false after error")
	}
}

// Only the latest fetch's reply loads the list; one from a fetch
// superseded by a reopen is dropped.
func TestHandleLoaded_DropsReplyFromSupersededFetch(t *testing.T) {
	d := deviceSelectorModel{}
	client := &spotify.Client{}
	d.open()
	d.fetch(context.Background(), client) // tab
	d.open()
	d.fetch(context.Background(), client) // esc, tab

	d.handleLoaded(devicesLoadedMsg{seq: 1, devices: twoDevices()})
	if !d.loading || len(d.devices) != 0 {
		t.Fatalf("stale reply applied: loading=%v devices=%d", d.loading, len(d.devices))
	}
	d.handleLoaded(devicesLoadedMsg{seq: 2, devices: twoDevices()})
	if d.loading || len(d.devices) != 2 {
		t.Errorf("latest reply not applied: loading=%v devices=%d", d.loading, len(d.devices))
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

	if len(out.devices) != 0 || !errors.Is(out.err, errTest) {
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

func TestHandleLoaded_DoesNotReorderMessageDevices(t *testing.T) {
	// The active device is last, so pinning it to the top reorders the list.
	devs := []spotify.Device{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B", Active: true},
	}
	d := deviceSelectorModel{}
	d.handleLoaded(devicesLoadedMsg{devices: devs})

	if d.devices[0].ID != "b" {
		t.Fatalf("active device not pinned to top: %+v", d.devices)
	}
	if devs[0].ID != "a" || devs[1].ID != "b" {
		t.Errorf("handleLoaded reordered the message's slice: %+v", devs)
	}
}
