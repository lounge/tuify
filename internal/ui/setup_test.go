package ui

import (
	"os"
	"testing"

	zone "github.com/lrstanley/bubblezone"

	"github.com/lounge/tuify/internal/testutil"
)

// TestMain initializes the bubblezone global manager once for the whole
// test binary. Without this, any View() call that hits zone.Mark panics
// with "manager not initialized". It then runs the tests under the
// goroutine leak check (see testutil.RunWithLeakCheck).
func TestMain(m *testing.M) {
	zone.NewGlobal()
	os.Exit(testutil.RunWithLeakCheck(m))
}
