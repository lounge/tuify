package ui

import "testing"

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
