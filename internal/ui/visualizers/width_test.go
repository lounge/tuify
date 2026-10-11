package visualizers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// TestEveryVisualizerFillsThePaneExactly pins the layout contract the ui
// package composes frames with: View(width, height) returns exactly height
// lines, each exactly width display cells once ANSI escapes are stripped.
// A wider line bleeds into whatever sits beside the pane; an extra line
// shifts everything below it.
func TestEveryVisualizerFillsThePaneExactly(t *testing.T) {
	t.Parallel()

	sizes := []struct{ w, h int }{
		{1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 2}, {5, 1},
		{1, 5}, {7, 3}, {40, 2}, {80, 24}, {200, 50},
	}
	for _, tc := range allVisualizers {
		for _, sz := range sizes {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, sz.w, sz.h), func(t *testing.T) {
				t.Parallel()

				v := newFedVisualizer(tc.new)
				sizeFor(v, sz.w, sz.h)
				for range 3 {
					v.View(sz.w, sz.h)
					v.Advance()
				}
				out := v.View(sz.w, sz.h)
				lines := strings.Split(out, "\n")
				if len(lines) != sz.h {
					t.Fatalf("%d lines, want %d", len(lines), sz.h)
				}
				for i, line := range lines {
					if w := runewidth.StringWidth(stripANSI(line)); w != sz.w {
						t.Errorf("line %d is %d cells, want %d: %q", i, w, sz.w, stripANSI(line))
					}
				}
			})
		}
	}
}
