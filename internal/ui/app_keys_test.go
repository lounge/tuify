package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// handleSearchKey tests — the "Enter on artist/album" regression lives here.
// The recent bug was a too-strict uriItem guard that silently swallowed Enter
// on drill-down items (artistItem, albumItem). The guard should only reject
// statusItem rows ("Loading more…", "No matching results") and nil.

func newSearchCtx(items []list.Item, selected int) (searchCtx, *bool, *list.Item) {
	l := list.New(items, list.NewDefaultDelegate(), 80, 20)
	l.SetFilteringEnabled(false)
	l.Select(selected)

	query := ""
	closed := false
	var playedItem list.Item
	sc := searchCtx{
		query: &query,
		list:  &l,
		close: func() { closed = true },
		play: func(it list.Item) tea.Cmd {
			playedItem = it
			return nil
		},
		onChange: func() tea.Cmd { return nil },
	}
	return sc, &closed, &playedItem
}

func pressEnter(t *testing.T, sc searchCtx) (tea.Cmd, bool) {
	t.Helper()
	return handleSearchKey(sc, tea.KeyMsg{Type: tea.KeyEnter})
}

func TestHandleSearchKey_Enter_DrillsArtistItem(t *testing.T) {
	items := []list.Item{artistItem{id: "a1", name: "Queen"}}
	sc, closed, played := newSearchCtx(items, 0)

	_, handled := pressEnter(t, sc)
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if !*closed {
		t.Error("expected sc.close() to be called so the search bar dismisses")
	}
	if *played == nil {
		t.Fatal("expected sc.play() to receive the artistItem")
	}
	if _, ok := (*played).(artistItem); !ok {
		t.Errorf("expected artistItem passed to play, got %T", *played)
	}
}

func TestHandleSearchKey_Enter_DrillsAlbumItem(t *testing.T) {
	items := []list.Item{albumItem{id: "al1", name: "A Night at the Opera"}}
	sc, _, played := newSearchCtx(items, 0)

	if _, handled := pressEnter(t, sc); !handled {
		t.Fatal("Enter should be handled")
	}
	if *played == nil {
		t.Fatal("expected play callback to fire for albumItem")
	}
	if _, ok := (*played).(albumItem); !ok {
		t.Errorf("expected albumItem passed to play, got %T", *played)
	}
}

func TestHandleSearchKey_Enter_PlaysTrackItem(t *testing.T) {
	items := []list.Item{trackItem{uri: "spotify:track:abc", name: "Bohemian Rhapsody"}}
	sc, _, played := newSearchCtx(items, 0)

	pressEnter(t, sc)
	if *played == nil {
		t.Fatal("expected play callback to receive trackItem")
	}
	if ti, ok := (*played).(trackItem); !ok || ti.uri != "spotify:track:abc" {
		t.Errorf("expected trackItem with URI, got %+v", *played)
	}
}

func TestHandleSearchKey_Enter_IgnoresStatusItem(t *testing.T) {
	items := []list.Item{statusItem{text: "Loading more…"}}
	sc, closed, played := newSearchCtx(items, 0)

	cmd, handled := pressEnter(t, sc)
	if !handled {
		t.Fatal("Enter should still be 'handled' (consumed) on a status row")
	}
	if cmd != nil {
		t.Error("expected nil cmd when Enter lands on status row")
	}
	if *closed {
		t.Error("search should NOT close when Enter lands on a status row — the user hasn't picked anything")
	}
	if *played != nil {
		t.Error("play callback must not fire for status items")
	}
}

// Overlay priority: while showHelp is true, every key is swallowed except
// close ('h'/'?'/'esc') and quit ('ctrl+c'/'q'). This documents the "help
// blocks all input" contract that the rest of the dispatcher relies on.

func TestHandleOverlayKey_HelpConsumesMostKeys(t *testing.T) {
	m := Model{showHelp: true, nowPlaying: &nowPlayingModel{}}

	cases := []string{"up", "down", "enter", " ", "n", "p", "/", "tab", "v"}
	for _, key := range cases {
		_, _, handled := m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if !handled {
			t.Errorf("help overlay should consume %q", key)
		}
	}
}

func TestHandleOverlayKey_HelpClosesOnToggleKeys(t *testing.T) {
	for _, key := range []string{"h", "?", "esc"} {
		m := Model{showHelp: true, nowPlaying: &nowPlayingModel{}}
		var msg tea.KeyMsg
		if key == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		after, _, handled := m.handleOverlayKey(msg)
		if !handled {
			t.Fatalf("%q should be handled by overlay", key)
		}
		if after.showHelp {
			t.Errorf("%q should close help overlay", key)
		}
	}
}

func TestHandleOverlayKey_NoOverlayFallsThrough(t *testing.T) {
	m := Model{nowPlaying: &nowPlayingModel{}}
	_, _, handled := m.handleOverlayKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if handled {
		t.Error("with no overlay active, handleOverlayKey must NOT consume keys — dispatch needs to reach handlePlaybackKey")
	}
}

// miniMode + visualizer interaction: 'v' must be a no-op while in miniMode,
// otherwise the visualizer renders underneath the compact layout and state
// diverges from the View() path (which short-circuits on miniMode).

func TestHandleNavigationKey_VizToggleBlockedInMiniMode(t *testing.T) {
	m := Model{
		miniMode:   true,
		nowPlaying: &nowPlayingModel{hasTrack: true, trackURI: "spotify:track:abc"},
		visualizer: newVisualizerModel(true),
	}
	after, cmd, handled := m.handleNavigationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})

	if !handled {
		t.Fatal("'v' must be consumed in miniMode so it doesn't reach other handlers")
	}
	if cmd != nil {
		t.Error("'v' in miniMode must not fire a command")
	}
	if after.visualizer.active {
		t.Error("visualizer must NOT activate while miniMode is on")
	}
}
