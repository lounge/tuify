package ui

import (
	"os"
	"testing"

	zone "github.com/lrstanley/bubblezone"
)

// TestMain initializes the bubblezone global manager once for the whole
// test binary. Without this, any View() call that hits zone.Mark panics
// with "manager not initialized".
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(m.Run())
}
