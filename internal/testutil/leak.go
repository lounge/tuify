package testutil

import (
	"bytes"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
)

// RunWithLeakCheck runs a package's tests through m and, once they have all
// finished, asks the runtime for its goroutineleak profile: goroutines
// parked on a channel, mutex, or select that no live goroutine can ever
// wake. Each one is a background worker some code path forgot to stop, so
// the helper prints the profile to stderr and turns a passing run into a
// failing one. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.RunWithLeakCheck(m)) }
//
// The profile is only consulted when the tests passed, so a leak report
// never buries the failure that caused it. The runtime cannot see leaks
// that are still reachable from a package-level variable, so this is a
// floor, not a proof.
func RunWithLeakCheck(m *testing.M) int {
	code := m.Run()
	if code != 0 {
		return code
	}
	report, err := goroutineLeaks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: goroutine leak check failed: %v\n", err)
		return 1
	}
	if report != "" {
		fmt.Fprintf(os.Stderr, "testutil: goroutines leaked after the tests finished:\n%s", report)
		return 1
	}
	return code
}

// goroutineLeaks renders the goroutineleak profile and returns it when it
// lists at least one goroutine, or "" when it is empty. Profile.Count does
// not cover this profile (it is computed on demand), so the total comes
// from the report's header line.
func goroutineLeaks() (string, error) {
	profile := pprof.Lookup("goroutineleak")
	if profile == nil {
		return "", fmt.Errorf("goroutineleak profile not available in this runtime")
	}
	var buf bytes.Buffer
	if err := profile.WriteTo(&buf, 1); err != nil {
		return "", err
	}
	var total int
	if _, err := fmt.Sscanf(buf.String(), "goroutineleak profile: total %d", &total); err != nil {
		return "", fmt.Errorf("unexpected profile header %q: %w", firstLine(buf.String()), err)
	}
	if total == 0 {
		return "", nil
	}
	return buf.String(), nil
}

func firstLine(s string) string {
	if i := bytes.IndexByte([]byte(s), '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
