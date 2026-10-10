package bootstrap

import (
	"context"
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
	lookups   int    // device-list requests
	onLookup  func() // called while answering each device-list request
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
