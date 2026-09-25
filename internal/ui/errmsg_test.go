package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/lounge/tuify/internal/spotify"
)

func TestUserMessage(t *testing.T) {
	until := time.Date(2026, 9, 25, 14, 3, 7, 0, time.Local)
	rle := &spotify.RateLimitedError{Until: until}
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"401", &spotify.APIError{Status: 401}, "Spotify session expired, restart tuify to log in"},
		{"403", &spotify.APIError{Status: 403, Body: []byte(`{"error":{"status":403}}`)}, "Spotify refused this action (Premium or playback restriction)"},
		{"404", &spotify.APIError{Status: 404}, "Not found on Spotify (is a device active?)"},
		{"429 with deadline", &spotify.APIError{Status: 429, Err: rle}, "Rate limited by Spotify until 14:03:07"},
		{"429 without deadline", &spotify.APIError{Status: 429}, "Rate limited by Spotify, try again shortly"},
		{"502", &spotify.APIError{Status: 502}, "Spotify is having trouble, try again shortly"},
		{"other status", &spotify.APIError{Status: 400}, "Spotify request failed (HTTP 400)"},
		{"wrapped APIError", fmt.Errorf("fetch: %w", &spotify.APIError{Status: 401}), "Spotify session expired, restart tuify to log in"},
		{"deadline", context.DeadlineExceeded, "Request timed out"},
		{"deadline in url.Error", &url.Error{Op: "Get", URL: "https://api.spotify.com/v1/me", Err: context.DeadlineExceeded}, "Request timed out"},
		{"net error", &url.Error{Op: "Get", URL: "https://api.spotify.com/v1/me", Err: netErr}, "Network error, check your connection"},
		{"plain passthrough", errors.New("no Spotify devices found — open Spotify on any device"), "no Spotify devices found — open Spotify on any device"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userMessage(tt.err); got != tt.want {
				t.Errorf("userMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
