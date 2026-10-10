package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/synctest"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// fullTrackPage loads n tracks into tv with more pages left.
func fullTrackPage(tv *trackView, n int) {
	items := make([]list.Item, n)
	for i := range items {
		items[i] = trackItem{uri: fmt.Sprintf("spotify:track:%d", i), name: "t"}
	}
	tv.Update(pageLoadedMsg{listID: tv.id, items: items, fetched: n, hasMore: true})
}

// Wheeling to the bottom of a loaded page asks for the next page at once,
// not on the next unrelated message.
func TestWheelToPageEndFetchesNextPage(t *testing.T) {
	m := newIntentTestModel()
	tv := newTrackView(m.rootCtx, m.client, "pid", "PL", 80, 20, false)
	m.viewStack = append(m.viewStack, tv)
	fullTrackPage(tv, 50)
	tv.list.Select(39) // 11 rows left: one notch down reaches the threshold

	wheel := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	handled, _, cmd := m.handleMouse(wheel)
	if !handled || cmd == nil {
		t.Fatalf("wheel to the page end: handled=%v cmd=%v, want a page fetch", handled, cmd != nil)
	}
	if !tv.loading {
		t.Error("the list is not loading after the wheel reached its end")
	}
}

func TestHalfPageDownFetchesNextPage(t *testing.T) {
	m := newIntentTestModel()
	m.vimMode = true
	m.height = 24
	tv := newTrackView(m.rootCtx, m.client, "pid", "PL", 80, 20, false)
	m.viewStack = append(m.viewStack, tv)
	fullTrackPage(tv, 50)
	tv.list.Select(38)

	_, cmd := pressKeys(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil || !tv.loading {
		t.Errorf("ctrl+d to the page end: cmd=%v loading=%v, want a page fetch", cmd != nil, tv.loading)
	}
}

func TestHalfPageOnEmptyListKeepsCursorValid(t *testing.T) {
	m := newIntentTestModel()
	m.height = 24
	tv := newTrackView(m.rootCtx, m.client, "pid", "PL", 80, 20, false)
	m.viewStack = append(m.viewStack, tv)
	tv.Update(pageLoadedMsg{listID: tv.id}) // an empty last page
	if n := len(tv.list.Items()); n != 0 {
		t.Fatalf("setup: %d rows, want none", n)
	}

	for _, dir := range []int{1, -1} {
		m, _ = m.halfPage(dir)
		if got := tv.list.Index(); got != 0 {
			t.Errorf("halfPage(%d) on an empty list: cursor %d, want 0", dir, got)
		}
	}
}

// Popping a screen cancels the page it is still loading instead of
// letting it run to listFetchTimeout.
func TestPopCancelsInFlightPage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newIntentTestModel()
		m.rootCtx = t.Context()
		load := func(ctx context.Context, offset int) ([]list.Item, int, bool, error) {
			<-ctx.Done()
			return nil, 0, false, ctx.Err()
		}
		pv := &playlistView{lazyList: newLazyList(m.rootCtx, load, 80, 20, false)}
		m.viewStack = append(m.viewStack, pv)
		done := runCmd(pv.Init())

		m, _ = pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		if len(m.viewStack) != 1 {
			t.Fatalf("esc did not pop: %d views", len(m.viewStack))
		}
		msg, _ := receiveNow(t, done, "page fetch").(pageLoadedMsg)
		if !errors.Is(msg.err, context.Canceled) {
			t.Errorf("fetch ended with %v, want context.Canceled", msg.err)
		}
	})
}

func TestPopCancelsSearchFetch(t *testing.T) {
	m := newIntentTestModel()
	sv := newSearchView(m.rootCtx, m.client, 80, 20, false)
	m.viewStack = append(m.viewStack, sv)
	sv.closeSearch()

	m, _ = pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.viewStack) != 1 || sv.ctx.Err() == nil {
		t.Errorf("popping search: views=%d ctx err=%v, want popped and cancelled", len(m.viewStack), sv.ctx.Err())
	}
}

// A loading row under help, the visualizer or mini mode is not on screen,
// so it must not keep the spinner ticking.
func TestNeedsSpinner_FalseWhileListHidden(t *testing.T) {
	for name, hide := range map[string]func(*Model){
		"help":       func(m *Model) { m.showHelp = true },
		"visualizer": func(m *Model) { m.visualizer.active = true },
		"mini mode":  func(m *Model) { m.miniMode = true },
	} {
		t.Run(name, func(t *testing.T) {
			m := newTickerTestModel(t)
			m.viewStack = append(m.viewStack, newPlaylistView(t.Context(), m.client, 80, 20, false))
			if !m.needsSpinner() {
				t.Fatal("setup: a loading list should spin")
			}
			hide(&m)
			if m.needsSpinner() {
				t.Error("needsSpinner() = true with the list hidden")
			}
		})
	}
}

// titleOf returns the title a tea.SetWindowTitle Cmd sets.
func titleOf(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("no title change")
	}
	v := reflect.ValueOf(cmd())
	if v.Kind() != reflect.String {
		t.Fatalf("Cmd produced %T, want a window title", cmd())
	}
	return v.String()
}

func TestWindowTitle_FollowsPlayback(t *testing.T) {
	m := newIntentTestModel()
	np := m.nowPlaying

	np.hasTrack, np.trackURI, np.track, np.artist = true, "spotify:track:a", "Song", "Band"
	if got := titleOf(t, m.windowTitle("", false)); got != "tuify — Song — Band" {
		t.Errorf("title for a new track = %q", got)
	}
	if cmd := m.windowTitle("spotify:track:a", true); cmd != nil {
		t.Error("a poll for the same track re-set the title")
	}

	// Playback stopped: the poll reports nothing playing.
	np.hasTrack = false
	if got := titleOf(t, m.windowTitle("spotify:track:a", true)); got != defaultWindowTitle {
		t.Errorf("title after playback stopped = %q, want %q", got, defaultWindowTitle)
	}
	if cmd := m.windowTitle("spotify:track:a", false); cmd != nil {
		t.Error("the title was reset again on the next empty poll")
	}

	// The same track resumes.
	np.hasTrack = true
	if got := titleOf(t, m.windowTitle("spotify:track:a", false)); got != "tuify — Song — Band" {
		t.Errorf("title after resuming = %q", got)
	}

	// An ad plays.
	np.trackURI = "spotify:ad:x"
	if got := titleOf(t, m.windowTitle("spotify:track:a", true)); got != defaultWindowTitle {
		t.Errorf("title during an ad = %q, want %q", got, defaultWindowTitle)
	}
}

// Through Update: a 204 after a track resets the terminal title.
func TestUpdate_NothingPlayingResetsTitle(t *testing.T) {
	m := newIntentTestModel()
	m = applyState(t, m, pstate("spotify:track:a", true, 1000))
	if cmd := m.windowTitle("spotify:track:a", true); cmd != nil {
		t.Fatal("setup: title would change on a repeat poll")
	}
	m.nowPlaying.pollSeq++
	// handleStateUpdate's only Cmd here is the title change (the home
	// view and the nil-state poll produce none).
	_, cmd := m.handleStateUpdate(playerStateMsg{seq: m.nowPlaying.pollSeq})
	if got := titleOf(t, cmd); got != defaultWindowTitle {
		t.Errorf("title = %q, want %q", got, defaultWindowTitle)
	}
}
