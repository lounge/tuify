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

// fakeSpotify answers the two calls reconnectHandler makes, in memory so
// it works inside a synctest bubble: the device list, and the transfer,
// whose body it records.
type fakeSpotify struct {
	mu        sync.Mutex
	transfers []string // PUT /v1/me/player bodies
	requests  int
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
		body = `{"devices":[{"id":"phone","name":"Phone","is_active":true},{"id":"tuify-id","name":"tuify"}]}`
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
				client := spotify.New(&http.Client{Transport: fake})
				client.PreferredDevice = "tuify"
				client.DeviceOverridden.Store(tc.overridden)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan struct{})
				go func() {
					reconnectHandler(ctx, client, "tuify")()
					close(done)
				}()

				// Nothing happens before the settle delay elapses.
				time.Sleep(2*time.Second - time.Millisecond)
				synctest.Wait()
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
