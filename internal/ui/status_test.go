package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

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

// nowPlayingModel status lifecycle — setters set the flag, clearStatusMsg
// resets it, and SetInfo/SetError replace a prior spinning state cleanly.

func newTestNowPlaying(t *testing.T) *nowPlayingModel {
	t.Helper()
	return newNowPlaying(&spotify.Client{})
}

func TestNowPlaying_SetSpinningInfo_SetsFlag(t *testing.T) {
	np := newTestNowPlaying(t)
	cmd := np.SetSpinningInfo("Switching to X")
	if !np.statusSpinning {
		t.Fatal("SetSpinningInfo should set statusSpinning=true")
	}
	if np.statusMsg != "Switching to X" {
		t.Errorf("statusMsg = %q, want %q", np.statusMsg, "Switching to X")
	}
	if np.statusIsError {
		t.Error("statusIsError should be false")
	}
	if cmd == nil {
		t.Error("SetSpinningInfo should return the auto-clear tick command")
	}
}

func TestNowPlaying_ClearStatusMsg_ResetsSpinning(t *testing.T) {
	np := newTestNowPlaying(t)
	np.SetSpinningInfo("Switching to X")

	if cmd := np.Update(clearStatusMsg{}); cmd != nil {
		t.Errorf("clearStatusMsg shouldn't return a command, got %v", cmd)
	}
	if np.statusSpinning {
		t.Error("clearStatusMsg should reset statusSpinning")
	}
	if np.statusMsg != "" {
		t.Errorf("clearStatusMsg should clear statusMsg, got %q", np.statusMsg)
	}
}

func TestNowPlaying_SetInfo_ResetsSpinningFromPriorCall(t *testing.T) {
	np := newTestNowPlaying(t)
	np.SetSpinningInfo("Switching to X")
	np.SetInfo("Copied link")

	if np.statusSpinning {
		t.Error("SetInfo should reset statusSpinning even after SetSpinningInfo")
	}
	if np.statusMsg != "Copied link" {
		t.Errorf("statusMsg = %q, want %q", np.statusMsg, "Copied link")
	}
}

func TestNowPlaying_SetError_ResetsSpinning(t *testing.T) {
	np := newTestNowPlaying(t)
	np.SetSpinningInfo("Switching to X")
	np.SetError("boom")

	if np.statusSpinning {
		t.Error("SetError should reset statusSpinning")
	}
	if !np.statusIsError {
		t.Error("SetError should set statusIsError")
	}
}

// Global spinner tick lifecycle — Model.Update must advance the shared
// loadingSpinner on spinner.TickMsg so every consumer sees fresh frames.
// We can't observe the frame directly (spinner.Model hides its state), but
// we can verify Update returns a non-nil command — i.e. schedules the next
// tick, keeping the chain alive.

func TestModel_SpinnerTick_SchedulesNextTick(t *testing.T) {
	m := Model{
		nowPlaying: newTestNowPlaying(t),
		client:     &spotify.Client{},
		viewStack:  []view{newHomeView(0, 0)},
	}
	_, cmd := m.Update(spinner.TickMsg{ID: loadingSpinner.ID()})
	if cmd == nil {
		t.Error("spinner.TickMsg should schedule the next tick — chain dies otherwise")
	}
}
