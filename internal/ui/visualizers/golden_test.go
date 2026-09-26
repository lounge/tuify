package visualizers

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden_frames.txt from the current output")

// TestGoldenFrames pins every visualizer's output byte for byte: frames 1,
// 10 and 60 at a small and a large terminal, fed the same inputs as the
// benchmarks. Frames are stored as SHA-256 so the file stays small; a
// mismatch means an optimization changed what is drawn. After an
// intentional change, run: go test ./internal/ui/visualizers -run Golden -update
//
// The hashes were checked to match on arm64 and amd64.
func TestGoldenFrames(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "golden_frames.txt")
	got := map[string]string{}
	for _, tc := range allVisualizers {
		for _, sz := range []struct{ w, h int }{{80, 24}, {250, 70}} {
			v := newFedVisualizer(tc.new)
			for frame := 1; frame <= 60; frame++ {
				v.Advance()
				out := v.View(sz.w, sz.h)
				if frame == 1 || frame == 10 || frame == 60 {
					key := fmt.Sprintf("%s/%dx%d/%d", tc.name, sz.w, sz.h, frame)
					got[key] = fmt.Sprintf("%x", sha256.Sum256([]byte(out)))
				}
			}
		}
	}

	if *update {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %s\n", k, got[k])
		}
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	want := map[string]string{}
	for line := range strings.Lines(string(data)) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			want[k] = v
		}
	}
	for k, g := range got {
		if w, ok := want[k]; !ok {
			t.Errorf("%s: no golden entry (run with -update)", k)
		} else if g != w {
			t.Errorf("%s: frame differs from golden", k)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("%s: golden entry no longer produced", k)
		}
	}
}
