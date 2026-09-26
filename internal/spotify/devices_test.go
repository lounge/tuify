package spotify

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type apiDevice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active bool   `json:"is_active"`
	Volume int    `json:"volume_percent"`
}

// newDevicesClient serves devices from GET /v1/me/player/devices.
func newDevicesClient(t *testing.T, preferred string, devices ...apiDevice) *Client {
	t.Helper()
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/me/player/devices" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if devices == nil {
			devices = []apiDevice{}
		}
		json.NewEncoder(w).Encode(map[string]any{"devices": devices})
	})
	c.PreferredDevice = preferred
	return c
}

func TestGetDevices_MapsFields(t *testing.T) {
	t.Parallel()

	c := newDevicesClient(t, "",
		apiDevice{ID: "a", Name: "Laptop", Type: "Computer", Active: true, Volume: 70},
		apiDevice{ID: "b", Name: "Kitchen", Type: "Speaker", Volume: 30},
	)
	got, err := c.GetDevices(t.Context())
	if err != nil {
		t.Fatalf("GetDevices: %v", err)
	}
	want := []Device{
		{ID: "a", Name: "Laptop", Type: "Computer", Active: true, Volume: 70},
		{ID: "b", Name: "Kitchen", Type: "Speaker", Volume: 30},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetDevices:\n got %+v\nwant %+v", got, want)
	}
}

// TestFindDevice covers the selection order: the preferred device (unless
// activeOnly), then the active device, then the first device.
func TestFindDevice(t *testing.T) {
	t.Parallel()

	tuify := apiDevice{ID: "t", Name: "tuify"}
	phone := apiDevice{ID: "p", Name: "Phone", Active: true}
	tv := apiDevice{ID: "tv", Name: "TV"}
	tests := []struct {
		name          string
		preferred     string
		activeOnly    bool
		devices       []apiDevice
		wantID        string
		wantActive    bool
		wantPreferred bool
		wantErr       string
	}{
		{"preferred beats active", "tuify", false, []apiDevice{phone, tuify}, "t", false, true, ""},
		{"active when preferred absent", "tuify", false, []apiDevice{tv, phone}, "p", true, false, ""},
		{"first device when none active", "", false, []apiDevice{tv, tuify}, "tv", false, false, ""},
		{"activeOnly ignores preferred", "tuify", true, []apiDevice{tuify, phone}, "p", true, false, ""},
		{"activeOnly with none active", "", true, []apiDevice{tv}, "", false, false, "no active Spotify device"},
		{"no devices", "tuify", false, nil, "", false, false, "no Spotify devices found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newDevicesClient(t, tc.preferred, tc.devices...)
			id, active, preferred, err := c.FindDevice(t.Context(), tc.activeOnly)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tc.wantID || active != tc.wantActive || preferred != tc.wantPreferred {
				t.Errorf("FindDevice = (%q, active=%v, preferred=%v), want (%q, %v, %v)",
					id, active, preferred, tc.wantID, tc.wantActive, tc.wantPreferred)
			}
		})
	}
}

func TestTransferPlayback_RequestShape(t *testing.T) {
	t.Parallel()

	for _, play := range []bool{true, false} {
		c, recorded := newRecordingClient(t)
		if err := c.TransferPlayback(t.Context(), "dev", play); err != nil {
			t.Fatalf("TransferPlayback(play=%v): %v", play, err)
		}
		want := []recordedRequest{{"PUT", "/v1/me/player", map[string]string{}, map[string]any{
			"device_ids": []any{"dev"},
			"play":       play,
		}}}
		if got := recorded(); !reflect.DeepEqual(got, want) {
			t.Errorf("play=%v requests:\n got %+v\nwant %+v", play, got, want)
		}
	}
}
