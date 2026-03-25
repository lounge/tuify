package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{30 * time.Second, "0:30"},
		{1 * time.Minute, "1:00"},
		{3*time.Minute + 45*time.Second, "3:45"},
		{10*time.Minute + 5*time.Second, "10:05"},
		{65 * time.Minute, "65:00"},
		{1*time.Hour + 2*time.Minute + 3*time.Second, "62:03"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// --- lazyList tests ---

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
