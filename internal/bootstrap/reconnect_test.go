package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lounge/tuify/internal/spotify"
)

const (
	devicesWithTuify    = `{"devices":[{"id":"phone","name":"Phone","is_active":true},{"id":"tuify-id","name":"tuify"}]}`
	devicesWithoutTuify = `{"devices":[{"id":"phone","name":"Phone","is_active":true}]}`
)

// fakeSpotify answers the two calls reconnectHandler makes, in memory so
// it works inside a synctest bubble: the device list, and the transfer,
// whose body it records.
type fakeSpotify struct {
	mu        sync.Mutex
	devices   []string // device-list bodies served in order; the last repeats. Empty: devicesWithTuify.
	transfers []string // PUT /v1/me/player bodies
	requests  int
	lookups   int      // device-list requests
	onLookup  func()   // called while answering each device-list request
	states    []string // player-state bodies served in order; the last repeats. Empty: 204.
	stateGets int      // player-state requests
	onState   func()   // called while answering each player-state request
	resumes   int      // PUT /v1/me/player/play requests
}

// nextDevices returns the device-list body for the next lookup. The
// caller holds mu.
func (f *fakeSpotify) nextDevices() string {
	f.lookups++
	if len(f.devices) == 0 {
		return devicesWithTuify
	}
	body := f.devices[0]
	if len(f.devices) > 1 {
		f.devices = f.devices[1:]
	}
	return body
}

func (f *fakeSpotify) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	body := ""
	status := http.StatusNoContent
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/me/player/devices":
		status = http.StatusOK
		body = f.nextDevices()
		if f.onLookup != nil {
			f.onLookup()
		}
	case req.Method == http.MethodPut && req.URL.Path == "/v1/me/player":
		b, _ := io.ReadAll(req.Body)
		f.transfers = append(f.transfers, strings.TrimSpace(string(b)))
	case req.Method == http.MethodGet && req.URL.Path == "/v1/me/player":
		f.stateGets++
		if f.onState != nil {
			f.onState()
		}
		if len(f.states) > 0 {
			status = http.StatusOK
			body = f.states[0]
			if len(f.states) > 1 {
				f.states = f.states[1:]
			}
		}
	case req.Method == http.MethodPut && req.URL.Path == "/v1/me/player/play":
		f.resumes++
	default:
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (f *fakeSpotify) snapshot() (requests int, transfers []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests, append([]string(nil), f.transfers...)
}

func (f *fakeSpotify) lookupCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookups
}

// TestReconnectHandler runs on synctest's fake clock, so the handler's
// fixed 2s settle delay costs nothing and can be asserted exactly.
func TestReconnectHandler(t *testing.T) {
	tests := []struct {
		name         string
		overridden   bool
		cancelEarly  bool
		wantTransfer bool
	}{
		{"transfers back to the preferred device", false, false, true},
		{"respects a manual device switch", true, false, false},
		{"shutdown during the settle delay aborts", false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := &fakeSpotify{}
				client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))
				client.DeviceOverridden.Store(tc.overridden)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan struct{})
				go func() {
					reconnectHandler(ctx, client, "tuify")()
					close(done)
				}()

				// Nothing happens before the settle delay elapses.
				synctest.Sleep(2*time.Second - time.Millisecond)
				if n, _ := fake.snapshot(); n != 0 {
					t.Fatalf("%d requests before the 2s settle delay", n)
				}
				if tc.cancelEarly {
					cancel()
				}
				<-done

				requests, transfers := fake.snapshot()
				if !tc.wantTransfer {
					if requests != 0 {
						t.Errorf("expected no Spotify calls, got %d (transfers %q)", requests, transfers)
					}
					return
				}
				want := `{"device_ids":["tuify-id"],"play":true}`
				if len(transfers) != 1 || transfers[0] != want {
					t.Errorf("transfers = %q, want one %s", transfers, want)
				}
			})
		})
	}
}

// FindDevice falls back to the active device, or any device, when tuify
// is not listed. The handler must never transfer there: it waits a settle
// delay and looks again while tuify registers, and gives up rather than
// start playback on the fallback.
func TestReconnectHandler_WaitsForPreferredDevice(t *testing.T) {
	tests := []struct {
		name          string
		devices       []string
		wantLookups   int
		wantTransfers int
	}{
		{"listed on the second lookup", []string{devicesWithoutTuify, devicesWithTuify}, 2, 1},
		{"never listed", []string{devicesWithoutTuify}, reconnectAttempts, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := &fakeSpotify{devices: tc.devices}
				client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))

				done := make(chan struct{})
				go func() {
					reconnectHandler(t.Context(), client, "tuify")()
					close(done)
				}()

				// The first lookup misses; nothing may be transferred before
				// the second settle delay has elapsed.
				synctest.Sleep(2*reconnectSettleDelay - time.Millisecond)
				if _, transfers := fake.snapshot(); len(transfers) != 0 {
					t.Fatalf("transferred after a missed lookup: %q", transfers)
				}
				<-done

				_, transfers := fake.snapshot()
				if got := fake.lookupCount(); got != tc.wantLookups {
					t.Errorf("device lookups = %d, want %d", got, tc.wantLookups)
				}
				if len(transfers) != tc.wantTransfers {
					t.Fatalf("transfers = %q, want %d", transfers, tc.wantTransfers)
				}
				for _, tr := range transfers {
					if !strings.Contains(tr, `"tuify-id"`) {
						t.Errorf("transferred to a fallback device: %s", tr)
					}
				}
			})
		})
	}
}

// Two "Authenticated as" lines inside the settle delay start two handler
// goroutines; only one may transfer.
func TestReconnectHandler_OverlappingTriggersTransferOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeSpotify{}
		client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))

		handler := reconnectHandler(t.Context(), client, "tuify")
		var wg sync.WaitGroup
		wg.Go(handler)
		time.Sleep(time.Second) // second trigger lands inside the 2s delay
		wg.Go(handler)
		wg.Wait()

		if _, transfers := fake.snapshot(); len(transfers) != 1 {
			t.Errorf("transfers = %d, want 1: overlapping reconnects both transferred", len(transfers))
		}

		// Once the first has finished, a later reconnect transfers again.
		handler()
		if _, transfers := fake.snapshot(); len(transfers) != 2 {
			t.Errorf("transfers = %d after a later reconnect, want 2", len(transfers))
		}
	})
}

// A manual device switch that lands while the device list is in flight
// must still stop the transfer: the doc promises the override stops the
// handler at any point, and transferring with play=true here would pull
// playback back from the device the user just picked.
func TestReconnectHandler_OverrideDuringLookupSkipsTransfer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeSpotify{}
		client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))
		fake.onLookup = func() { client.DeviceOverridden.Store(true) }

		reconnectHandler(t.Context(), client, "tuify")()

		if got := fake.lookupCount(); got != 1 {
			t.Errorf("device lookups = %d, want 1 (the switch settles the reconnect)", got)
		}
		if _, transfers := fake.snapshot(); len(transfers) != 0 {
			t.Errorf("transferred after a manual switch during the lookup: %q", transfers)
		}
	})
}

// playerState is a GET /v1/me/player body for a track on device.
func playerState(device string, playing bool) string {
	return fmt.Sprintf(`{"is_playing":%v,"device":{"name":%q},"item":{"name":"Song","uri":"spotify:track:1","duration_ms":290013,"artists":[{"name":"Band"}]}}`, playing, device)
}

func (f *fakeSpotify) resumeCounts() (stateGets, resumes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stateGets, f.resumes
}

// After a broken session Spotify can accept a transfer with play=true and
// leave the restarted librespot paused. When the user wanted playback
// running, the handler checks after the transfer and resumes; a pause the
// user made is carried across instead, and an unknown intent (startup)
// keeps the old transfer-and-play behaviour without the follow-up.
func TestReconnectHandler_PlayIntent(t *testing.T) {
	tests := []struct {
		name          string
		intent        *bool // nil: never set
		states        []string
		pauseDuring   bool // the user pauses during the check delay
		wantPlay      bool
		wantStateGets int
		wantResumes   int
	}{
		{
			name:   "paused after the transfer is resumed",
			intent: new(true), states: []string{playerState("tuify", false), playerState("tuify", true)},
			wantPlay: true, wantStateGets: 2, wantResumes: 1,
		},
		{
			name:   "already playing is left alone",
			intent: new(true), states: []string{playerState("tuify", true)},
			wantPlay: true, wantStateGets: 1, wantResumes: 0,
		},
		{
			name:   "nothing reported is resumed",
			intent: new(true), states: nil,
			wantPlay: true, wantStateGets: resumeChecks, wantResumes: resumeChecks,
		},
		{
			name:   "playback on another device is not pulled back",
			intent: new(true), states: []string{playerState("Phone", false)},
			wantPlay: true, wantStateGets: 1, wantResumes: 0,
		},
		{
			name:   "a pause during the check wins",
			intent: new(true), states: []string{playerState("tuify", false)}, pauseDuring: true,
			wantPlay: true, wantStateGets: 0, wantResumes: 0,
		},
		{
			name:     "a user pause survives the reconnect",
			intent:   new(false),
			wantPlay: false, wantStateGets: 0, wantResumes: 0,
		},
		{
			name:     "unknown intent transfers with play and does not follow up",
			wantPlay: true, wantStateGets: 0, wantResumes: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := &fakeSpotify{states: tc.states}
				client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))
				if tc.intent != nil {
					client.SetPlayIntent(*tc.intent)
				}

				done := make(chan struct{})
				go func() {
					reconnectHandler(t.Context(), client, "tuify")()
					close(done)
				}()
				if tc.pauseDuring {
					synctest.Sleep(reconnectSettleDelay + time.Second)
					client.SetPlayIntent(false)
				}
				<-done

				_, transfers := fake.snapshot()
				want := fmt.Sprintf(`{"device_ids":["tuify-id"],"play":%v}`, tc.wantPlay)
				if len(transfers) != 1 || transfers[0] != want {
					t.Fatalf("transfers = %q, want one %s", transfers, want)
				}
				gets, resumes := fake.resumeCounts()
				if gets != tc.wantStateGets || resumes != tc.wantResumes {
					t.Errorf("player-state reads = %d, resumes = %d; want %d and %d", gets, resumes, tc.wantStateGets, tc.wantResumes)
				}
			})
		})
	}
}

// The 17:39 incident: librespot was killed and restarted while the first
// reconnect was still checking playback. The restart's reconnect must
// still transfer, and the older check must stop rather than resume the
// device the newer reconnect now owns.
func TestReconnectHandler_RestartDuringCheckStillTransfers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fake := &fakeSpotify{states: []string{playerState("tuify", false)}}
		client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))
		client.SetPlayIntent(true)
		handler := reconnectHandler(t.Context(), client, "tuify")

		var wg sync.WaitGroup
		wg.Go(handler)
		// First transfer lands at 2s; its first check would run at 5s.
		synctest.Sleep(reconnectSettleDelay + time.Second)
		wg.Go(handler)
		wg.Wait()

		if _, transfers := fake.snapshot(); len(transfers) != 2 {
			t.Fatalf("transfers = %d, want 2: the restart's reconnect was dropped", len(transfers))
		}
		// Only the newer reconnect checks: every read and resume is its own.
		gets, resumes := fake.resumeCounts()
		if gets != resumeChecks || resumes != resumeChecks {
			t.Errorf("player-state reads = %d, resumes = %d; want %d each from the newer reconnect only", gets, resumes, resumeChecks)
		}
	})
}

// The check reads the player state before it resumes, and the user can
// pause or switch device, or librespot reconnect again, while that read
// is in flight. The paused state it then reads is the user's own pause or
// another device's business, so resuming would override them; the
// conditions are asked again once the read is back.
func TestReconnectHandler_ChangeDuringStateReadStopsResume(t *testing.T) {
	tests := []struct {
		name          string
		during        func(client *spotify.Client, handler func(), wg *sync.WaitGroup)
		wantTransfers int
		wantResumes   int
	}{
		{
			name:          "pause",
			during:        func(client *spotify.Client, _ func(), _ *sync.WaitGroup) { client.SetPlayIntent(false) },
			wantTransfers: 1, wantResumes: 0,
		},
		{
			name:          "device switch",
			during:        func(client *spotify.Client, _ func(), _ *sync.WaitGroup) { client.DeviceOverridden.Store(true) },
			wantTransfers: 1, wantResumes: 0,
		},
		{
			// The newer reconnect transfers and checks for itself; every
			// resume is its own.
			name: "newer reconnect",
			during: func(_ *spotify.Client, handler func(), wg *sync.WaitGroup) {
				wg.Go(handler)
				// Let it take its generation before the read returns.
				time.Sleep(time.Millisecond)
			},
			wantTransfers: 2, wantResumes: resumeChecks,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := &fakeSpotify{states: []string{playerState("tuify", false)}}
				client := spotify.New(&http.Client{Transport: fake}, spotify.WithPreferredDevice("tuify"))
				client.SetPlayIntent(true)
				handler := reconnectHandler(t.Context(), client, "tuify")
				var wg sync.WaitGroup
				fake.onState = func() {
					fake.onState = nil // only the first read; fake.mu is held
					tc.during(client, handler, &wg)
				}

				wg.Go(handler)
				wg.Wait()

				if _, transfers := fake.snapshot(); len(transfers) != tc.wantTransfers {
					t.Errorf("transfers = %d, want %d", len(transfers), tc.wantTransfers)
				}
				if _, resumes := fake.resumeCounts(); resumes != tc.wantResumes {
					t.Errorf("resumes = %d, want %d", resumes, tc.wantResumes)
				}
			})
		})
	}
}
