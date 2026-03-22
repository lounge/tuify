package ui

import "testing"

func TestIsTrackURI(t *testing.T) {
	tests := []struct {
		uri  string
		want bool
	}{
		{"spotify:track:abc123", true},
		{"spotify:episode:abc123", false},
		{"spotify:track:", true},
		{"", false},
		{"spotify:tracks:abc", false},
	}

	for _, tt := range tests {
		if got := isTrackURI(tt.uri); got != tt.want {
			t.Errorf("isTrackURI(%q) = %v, want %v", tt.uri, got, tt.want)
		}
	}
}

func TestTrackIDFromURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"spotify:track:abc123", "abc123"},
		{"spotify:track:", ""},
		{"notauri", "notauri"}, // no prefix to trim
	}

	for _, tt := range tests {
		if got := trackIDFromURI(tt.uri); got != tt.want {
			t.Errorf("trackIDFromURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}
