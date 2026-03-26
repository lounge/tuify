package ui

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{30 * time.Second, "0:30"},
		{1 * time.Minute, "1:00"},
		{3*time.Minute + 45*time.Second, "3:45"},
		{10*time.Minute + 5*time.Second, "10:05"},
		{65 * time.Minute, "65:00"},
		{1*time.Hour + 2*time.Minute + 3*time.Second, "62:03"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestIsPlayableURI(t *testing.T) {
	tests := []struct {
		uri  string
		want bool
	}{
		{"spotify:track:abc123", true},
		{"spotify:episode:abc123", true},
		{"spotify:track:", true},
		{"spotify:episode:", true},
		{"", false},
		{"spotify:tracks:abc", false},
		{"spotify:show:abc", false},
	}

	for _, tt := range tests {
		if got := isPlayableURI(tt.uri); got != tt.want {
			t.Errorf("isPlayableURI(%q) = %v, want %v", tt.uri, got, tt.want)
		}
	}
}

func TestIDFromURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"spotify:track:abc123", "abc123"},
		{"spotify:episode:xyz789", "xyz789"},
		{"spotify:track:", ""},
		{"notauri", "notauri"},
	}

	for _, tt := range tests {
		if got := idFromURI(tt.uri); got != tt.want {
			t.Errorf("idFromURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}
