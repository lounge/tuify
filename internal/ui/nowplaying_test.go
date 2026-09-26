package ui

import (
	"testing"

	"github.com/lounge/tuify/internal/spotify"
)

// nowPlayingModel status lifecycle — setters set the flag, clearStatusMsg
// resets it, and setInfo/setError replace a prior spinning state cleanly.

func newTestNowPlaying(t *testing.T) *nowPlayingModel {
	t.Helper()
	return newNowPlaying(&spotify.Client{})
}

func TestNowPlaying_SetSpinningInfo_SetsFlag(t *testing.T) {
	np := newTestNowPlaying(t)
	cmd := np.setSpinningInfo("Switching to X")
	if !np.statusSpinning {
		t.Fatal("setSpinningInfo should set statusSpinning=true")
	}
	if np.statusMsg != "Switching to X" {
		t.Errorf("statusMsg = %q, want %q", np.statusMsg, "Switching to X")
	}
	if np.statusIsError {
		t.Error("statusIsError should be false")
	}
	if cmd == nil {
		t.Error("setSpinningInfo should return the auto-clear tick command")
	}
}

func TestNowPlaying_ClearStatusMsg_ResetsSpinning(t *testing.T) {
	np := newTestNowPlaying(t)
	np.setSpinningInfo("Switching to X")

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
	np.setSpinningInfo("Switching to X")
	np.setInfo("Copied link")

	if np.statusSpinning {
		t.Error("setInfo should reset statusSpinning even after setSpinningInfo")
	}
	if np.statusMsg != "Copied link" {
		t.Errorf("statusMsg = %q, want %q", np.statusMsg, "Copied link")
	}
}

func TestNowPlaying_SetError_ResetsSpinning(t *testing.T) {
	np := newTestNowPlaying(t)
	np.setSpinningInfo("Switching to X")
	np.setError("boom")

	if np.statusSpinning {
		t.Error("setError should reset statusSpinning")
	}
	if !np.statusIsError {
		t.Error("setError should set statusIsError")
	}
}
