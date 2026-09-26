package ui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

// triggerLoad fires once at most 10 loaded rows remain from the cursor
// (len(items)-Index() <= 10). Pins both sides of the boundary.
func TestLazyList_TriggerLoad_Boundary(t *testing.T) {
	const n = 40
	tests := []struct {
		cursor int
		want   bool
	}{
		{cursor: 0, want: false},
		{cursor: n - 11, want: false},
		{cursor: n - 10, want: true},
		{cursor: n - 1, want: true},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("cursor=%d", tc.cursor), func(t *testing.T) {
			ll := newTestLazyList()
			ll.loading, ll.hasMore = false, true
			items := make([]list.Item, n)
			for i := range items {
				items[i] = trackItem{uri: fmt.Sprintf("spotify:track:%d", i), name: "t"}
			}
			ll.items = items
			ll.list.SetItems(items)
			ll.list.Select(tc.cursor)
			if ll.list.Index() != tc.cursor {
				t.Fatalf("setup: cursor = %d, want %d", ll.list.Index(), tc.cursor)
			}

			if got := ll.triggerLoad(); got != tc.want {
				t.Errorf("triggerLoad() = %v with %d rows left, want %v", got, n-tc.cursor, tc.want)
			}
			wantLen := n
			if tc.want {
				wantLen++ // the loading row
			}
			if len(ll.items) != wantLen || ll.loading != tc.want {
				t.Errorf("items=%d loading=%v, want %d/%v", len(ll.items), ll.loading, wantLen, tc.want)
			}
		})
	}
}
