package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
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

// statusItem rendering — the spinning flag must actually change the output.
// This is what prevents a future refactor from silently turning the spinner
// off on every loading row.

func TestStatusItem_Title_NonSpinningOmitsSpinnerFrame(t *testing.T) {
	item := statusItem{text: "No matching results"}
	got := item.Title()
	// The rendered width should equal the rendered width of the plain text —
	// anything wider means we snuck a prefix in.
	want := lipgloss.Width(loadingStyle.Render("No matching results"))
	if lipgloss.Width(got) != want {
		t.Errorf("non-spinning Title width = %d, want %d (got %q)", lipgloss.Width(got), want, got)
	}
}

func TestStatusItem_Title_SpinningIncludesFrame(t *testing.T) {
	plain := statusItem{text: "Loading…"}
	spinning := statusItem{text: "Loading…", spinning: true}

	plainW := lipgloss.Width(plain.Title())
	spinW := lipgloss.Width(spinning.Title())

	// Spinner frame + space is exactly 2 extra display columns.
	if diff := spinW - plainW; diff != 2 {
		t.Errorf("spinning Title should add 2 cols (spinner + space), got diff %d (plain=%q spinning=%q)",
			diff, plain.Title(), spinning.Title())
	}
	// Make sure the underlying text is still present.
	if !strings.Contains(spinning.Title(), "Loading") {
		t.Errorf("spinning Title lost its text: %q", spinning.Title())
	}
}

func TestStatusItem_Title_ErrorIgnoresSpinning(t *testing.T) {
	// An error row with spinning=true accidentally set should still render
	// as a plain error — we don't want a spinner on failure states.
	item := statusItem{text: "Failed", isError: true, spinning: true}
	got := item.Title()
	if lipgloss.Width(got) != lipgloss.Width(errorStyle.Render("Failed")) {
		t.Errorf("error row should not gain a spinner prefix, got %q", got)
	}
}

// renderStatusLine — the shared formatter used by both the full and mini
// now-playing views. Pins the three branches (plain / spinning / error).

func TestRenderStatusLine_PlainInfo(t *testing.T) {
	got := renderStatusLine("Copied link", false, false)
	if strings.Contains(got, loadingSpinner.View()) {
		t.Errorf("plain info shouldn't include the spinner, got %q", got)
	}
	if !strings.Contains(got, "Copied link") {
		t.Errorf("output lost the message text, got %q", got)
	}
}

func TestRenderStatusLine_Spinning(t *testing.T) {
	plain := renderStatusLine("Switching to Kitchen", false, false)
	spin := renderStatusLine("Switching to Kitchen", true, false)
	if lipgloss.Width(spin)-lipgloss.Width(plain) != 2 {
		t.Errorf("spinning should add exactly 2 cols; plain=%q spin=%q", plain, spin)
	}
}

func TestRenderStatusLine_Error(t *testing.T) {
	got := renderStatusLine("Failed to copy", false, true)
	// Must carry the error foreground; the simplest check is that the
	// rendered output matches what errorStyle produces for the same text.
	if got != errorStyle.Render("Failed to copy") {
		t.Errorf("error styling mismatch; got %q want %q", got, errorStyle.Render("Failed to copy"))
	}
}
