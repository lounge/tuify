package auth

import (
	"os"
	"testing"

	"github.com/lounge/tuify/internal/testutil"
)

// TestMain fails the package when any goroutine started during the tests
// is still parked, unreachable, after they finish. See
// testutil.RunWithLeakCheck.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithLeakCheck(m))
}
