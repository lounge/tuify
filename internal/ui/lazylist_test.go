package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

func newTestLazyList() lazyList {
	return newLazyList(80, 20, false)
}

func TestLazyList_TriggerLoad_NearEnd(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	// Set items so cursor is within 10 of end
	ll.items = make([]list.Item, 5)
	ll.list.SetItems(ll.items)

	if !ll.triggerLoad() {
		t.Error("triggerLoad should fire when cursor is near end")
	}
	if !ll.loading {
		t.Error("loading should be true after triggerLoad")
	}
}

func TestLazyList_TriggerLoad_NotNearEnd(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	// 20 items, cursor at 0 → 20 items remaining, not near end
	items := make([]list.Item, 20)
	for i := range items {
		items[i] = trackItem{uri: "u", name: "t"}
	}
	ll.items = items
	ll.list.SetItems(items)

	if ll.triggerLoad() {
		t.Error("triggerLoad should not fire when cursor is far from end")
	}
}

func TestLazyList_TriggerLoad_AlreadyLoading(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = true
	ll.hasMore = true
	ll.items = make([]list.Item, 3)

	if ll.triggerLoad() {
		t.Error("triggerLoad should not fire when already loading")
	}
}

func TestLazyList_TriggerLoad_NoMore(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = false
	ll.items = make([]list.Item, 3)

	if ll.triggerLoad() {
		t.Error("triggerLoad should not fire when hasMore is false")
	}
}

func TestLazyList_ApplyFilter_Matches(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = false
	ll.searching = true
	ll.items = []list.Item{
		trackItem{name: "Hello World", uri: "u1"},
		trackItem{name: "Goodbye Moon", uri: "u2"},
		trackItem{name: "Hello Again", uri: "u3"},
	}
	ll.searchQuery = "hello"

	ll.applyFilter()

	displayed := ll.list.Items()
	if len(displayed) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(displayed))
	}
}

func TestLazyList_ApplyFilter_NoMatches(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = false
	ll.items = []list.Item{
		trackItem{name: "Song A", uri: "u1"},
	}
	ll.searchQuery = "zzzzz"

	ll.applyFilter()

	displayed := ll.list.Items()
	if len(displayed) != 1 {
		t.Fatalf("expected 1 status item, got %d", len(displayed))
	}
	si, ok := displayed[0].(statusItem)
	if !ok || si.text != "No matching results" {
		t.Errorf("expected 'No matching results' statusItem, got %+v", displayed[0])
	}
}

func TestLazyList_ApplyFilter_PendingAppendsLoadingItem(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	ll.searching = true
	ll.items = []list.Item{
		trackItem{name: "Hello World", uri: "u1"},
		trackItem{name: "Goodbye Moon", uri: "u2"},
	}
	ll.searchQuery = "hello"

	ll.applyFilter()

	displayed := ll.list.Items()
	if len(displayed) != 2 {
		t.Fatalf("expected 1 match + loading item, got %d", len(displayed))
	}
	si, ok := displayed[1].(statusItem)
	if !ok || si.text != "Loading more…" {
		t.Errorf("expected 'Loading more…' status item, got %+v", displayed[1])
	}
}

func TestLazyList_ApplyFilter_NoMatchesWhilePendingShowsSearching(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	ll.items = []list.Item{
		trackItem{name: "Song A", uri: "u1"},
	}
	ll.searchQuery = "zzzzz"

	ll.applyFilter()

	displayed := ll.list.Items()
	si, ok := displayed[0].(statusItem)
	if !ok || si.text != "Searching…" {
		t.Errorf("expected 'Searching…' status item while pending, got %+v", displayed[0])
	}
}

func TestLazyList_ApplyFilter_EmptyQuery(t *testing.T) {
	ll := newTestLazyList()
	ll.items = []list.Item{
		trackItem{name: "Song A", uri: "u1"},
		trackItem{name: "Song B", uri: "u2"},
	}
	ll.searchQuery = ""

	ll.applyFilter()

	if len(ll.list.Items()) != 2 {
		t.Errorf("empty query should show all items, got %d", len(ll.list.Items()))
	}
}

func TestLazyList_SelectByURI_Found(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.items = []list.Item{
		trackItem{name: "A", uri: "u1"},
		trackItem{name: "B", uri: "u2"},
		trackItem{name: "C", uri: "u3"},
	}
	ll.list.SetItems(ll.items)

	shouldFetch := ll.selectByURI("u2")
	if shouldFetch {
		t.Error("should not need to fetch more when URI is found")
	}
	if ll.syncURI != "" {
		t.Error("syncURI should be cleared")
	}
	if ll.list.Index() != 1 {
		t.Errorf("expected selection at index 1, got %d", ll.list.Index())
	}
}

func TestLazyList_SelectByURI_NotFound_HasMore(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	ll.items = []list.Item{
		trackItem{name: "A", uri: "u1"},
	}
	ll.list.SetItems(ll.items)

	shouldFetch := ll.selectByURI("u99")
	if !shouldFetch {
		t.Error("should return true to fetch more when URI not found and hasMore")
	}
	if ll.syncURI != "u99" {
		t.Errorf("syncURI should be set to u99, got %q", ll.syncURI)
	}
}

func TestLazyList_ResolveSync_Found(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.syncURI = "u2"
	ll.items = []list.Item{
		trackItem{name: "A", uri: "u1"},
		trackItem{name: "B", uri: "u2"},
	}
	ll.list.SetItems(ll.items)

	shouldFetch := ll.resolveSync()
	if shouldFetch {
		t.Error("should not need to fetch more when sync URI is found")
	}
	if ll.syncURI != "" {
		t.Error("syncURI should be cleared after resolve")
	}
}

func TestLazyList_ResolveSync_NotFound_NoMore(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = false
	ll.syncURI = "u99"
	ll.items = []list.Item{
		trackItem{name: "A", uri: "u1"},
	}
	ll.list.SetItems(ll.items)

	shouldFetch := ll.resolveSync()
	if shouldFetch {
		t.Error("should not fetch more when hasMore is false")
	}
	if ll.syncURI != "" {
		t.Error("syncURI should be cleared when no more data")
	}
}

func TestLazyList_ResolveSync_Empty(t *testing.T) {
	ll := newTestLazyList()
	ll.syncURI = ""

	if ll.resolveSync() {
		t.Error("resolveSync should return false when syncURI is empty")
	}
}

func TestLazyList_Append_SearchMode(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.searching = true
	ll.searchQuery = "song"
	ll.items = nil

	items := []list.Item{
		trackItem{name: "Song A", uri: "u1"},
		trackItem{name: "Other", uri: "u2"},
	}

	needMore := ll.append(items, 2, true)
	if !needMore {
		t.Error("in search mode with hasMore, should return true to fetch more")
	}
	if !ll.loading {
		t.Error("loading should be set back to true")
	}
}

func TestLazyList_Append_NormalMode(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.items = nil

	items := []list.Item{
		trackItem{name: "A", uri: "u1"},
	}

	needMore := ll.append(items, 1, false)
	if needMore {
		t.Error("in normal mode should return false")
	}
	if ll.offset != 1 {
		t.Errorf("offset should be 1, got %d", ll.offset)
	}
}

// TestLazyList_ApplyFilter_FromDeepCursor_NoViewPanic reproduces the bubbles/list
// panic we hit when the user scrolled to a far index in a large playlist, then
// pressed "/" and typed a query that filtered the list down to a handful of
// matches. After SetItems the paginator's Page*PerPage could still point past
// the new, smaller item slice, so the next View() did items[Page*PerPage:len]
// with start > end and crashed. applyFilter now resets the cursor before
// SetItems; this test pins that behavior by calling View() to force rendering.
func TestLazyList_ApplyFilter_FromDeepCursor_NoViewPanic(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = false
	ll.searching = true

	// Big backing list (~matches the real-world scenario where the user had
	// scrolled past index 4000 in a multi-page playlist).
	items := make([]list.Item, 5000)
	for i := range items {
		items[i] = trackItem{
			name: "Track " + string(rune('A'+i%26)),
			uri:  "uri" + string(rune('a'+i%26)) + string(rune('0'+i%10)),
		}
	}
	// Place one item that matches the upcoming query at a known index.
	items[12] = trackItem{name: "XYZ match", uri: "xyz-match"}
	ll.items = items
	ll.list.SetItems(ll.items)

	// Simulate the user scrolling to a far index before filtering.
	ll.list.Select(4020)

	// Filter to a small match set. This is the exact shape of the panic repro.
	ll.searchQuery = "XYZ"
	ll.applyFilter()

	// Force the renderer — this is what panicked before the fix.
	_ = ll.View()

	if got := len(ll.list.Items()); got != 1 {
		t.Fatalf("expected 1 filtered item, got %d", got)
	}
}

// TestLazyList_ApplyFilter_ShrinkThenGrow_NoViewPanic covers the related case
// where the filter first shrinks the view, then a background page arrives and
// applyFilter runs again with more items. View() must stay safe across both.
func TestLazyList_ApplyFilter_ShrinkThenGrow_NoViewPanic(t *testing.T) {
	ll := newTestLazyList()
	ll.loading = false
	ll.hasMore = true
	ll.searching = true

	initial := make([]list.Item, 2000)
	for i := range initial {
		initial[i] = trackItem{name: "Track", uri: "u"}
	}
	initial[5] = trackItem{name: "needle", uri: "needle-1"}
	ll.items = initial
	ll.list.SetItems(ll.items)
	ll.list.Select(1800)

	ll.searchQuery = "needle"
	ll.applyFilter()
	_ = ll.View()

	// A new page of items arrives in the background.
	more := make([]list.Item, 2000)
	for i := range more {
		more[i] = trackItem{name: "Track", uri: "u"}
	}
	more[100] = trackItem{name: "needle", uri: "needle-2"}
	ll.append(more, len(more), false)

	_ = ll.View()

	if got := len(ll.list.Items()); got != 2 {
		t.Fatalf("expected 2 filtered items after append, got %d", got)
	}
}
