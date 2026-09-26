package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
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

// Key dispatch through Model.Update: pins the tier order in handleKeyMsg
// (overlay → search input → vim → quit → playback → navigation) and the
// capability-interface routing that lets screens be added without editing
// the shell.

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func pressKeys(t *testing.T, m Model, keys ...tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var updated tea.Model
		updated, cmd = m.Update(k)
		m = updated.(Model)
	}
	return m, cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestUpdate_HelpOverlayTakesPriority(t *testing.T) {
	m, _ := pressKeys(t, newIntentTestModel(), runeKey("?"))
	if !m.showHelp {
		t.Fatal("? should open the help overlay")
	}

	// While help is open, a navigation key is swallowed by the overlay.
	before := len(m.viewStack)
	m, _ = pressKeys(t, m, runeKey("m"))
	if m.miniMode || len(m.viewStack) != before || !m.showHelp {
		t.Error("keys other than close/quit must be swallowed while help is open")
	}

	m, _ = pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showHelp {
		t.Error("esc should close the help overlay")
	}

	m, _ = pressKeys(t, m, runeKey("?"))
	if _, cmd := pressKeys(t, m, runeKey("q")); !isQuit(cmd) {
		t.Error("q should quit even while help is open")
	}
}

func TestUpdate_EscPopsViewStack(t *testing.T) {
	m := newIntentTestModel()
	m.viewStack = append(m.viewStack, newPlaylistView(m.rootCtx, m.client, 80, 20, false))

	m, _ = pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.viewStack) != 1 {
		t.Fatalf("viewStack len = %d after esc, want 1", len(m.viewStack))
	}
	if _, ok := m.currentView().(*homeView); !ok {
		t.Errorf("top of viewStack = %T, want *homeView", m.currentView())
	}
}

func TestUpdate_EnterOnHomeOpensSelectedScreen(t *testing.T) {
	m := newIntentTestModel()

	// Home's cursor starts on "Search"; Enter emits the open intent, which
	// the shell turns into a pushed view on the next Update.
	m, cmd := pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on home should emit an open intent")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if _, ok := m.currentView().(*searchView); !ok {
		t.Errorf("top of viewStack = %T, want *searchView", m.currentView())
	}
}

func TestUpdate_EnterIgnoredInMiniMode(t *testing.T) {
	m := newIntentTestModel()
	m.miniMode = true

	m, cmd := pressKeys(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || len(m.viewStack) != 1 {
		t.Error("enter in mini mode must not open a screen")
	}
}

func TestUpdate_VTogglesVisualizer(t *testing.T) {
	m := newIntentTestModel()
	m.nowPlaying.hasTrack = true
	m.nowPlaying.trackURI = "spotify:track:abc"
	m.visualizer.ctx = t.Context() // NewModel wires this; toggle starts lyric/image loads

	m, _ = pressKeys(t, m, runeKey("v"))
	if !m.visualizer.active {
		t.Fatal("v with a playable track should open the visualizer")
	}
	m, _ = pressKeys(t, m, runeKey("v"))
	if m.visualizer.active {
		t.Error("a second v should close the visualizer")
	}
}

func TestUpdate_SlashOpensViewOwnedSearchInput(t *testing.T) {
	m := newIntentTestModel()
	sv := newSearchView(m.rootCtx, m.client, 80, 20, false)
	m.viewStack = append(m.viewStack, sv)

	m, _ = pressKeys(t, m, runeKey("/"))
	if !sv.searching {
		t.Fatal("/ on the search view should open its search input")
	}

	// With the input open, "n" is typed into the query instead of
	// triggering the next-track playback shortcut.
	_, cmd := pressKeys(t, m, runeKey("n"))
	if sv.searchQuery != "n" {
		t.Errorf("searchQuery = %q, want %q", sv.searchQuery, "n")
	}
	if cmd != nil {
		t.Error("a one-rune query must not start a search or a playback command")
	}
}

func TestUpdate_SlashOpensLocalFilterOnPlaylists(t *testing.T) {
	m := newIntentTestModel()
	pv := newPlaylistView(m.rootCtx, m.client, 80, 20, false)
	m.viewStack = append(m.viewStack, pv)

	m, _ = pressKeys(t, m, runeKey("/"))
	if !pv.searching {
		t.Fatal("/ on the playlists screen should open the local filter")
	}
	pressKeys(t, m, runeKey("r"), runeKey("o"))
	if pv.searchQuery != "ro" {
		t.Errorf("filter query = %q, want %q", pv.searchQuery, "ro")
	}
}

func TestNewModel_PanicsOnNilClient(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewModel with a nil client should panic")
		}
	}()
	NewModel(t.Context(), nil)
}

func TestPlaylistAndPodcastSearch_KeepsFetchingWhileFiltering(t *testing.T) {
	ctx, client := t.Context(), &spotify.Client{}
	tests := []struct {
		name string
		v    interface {
			view
			SearchableList() *lazyList
		}
		loaded tea.Msg
	}{
		{"playlists", newPlaylistView(ctx, client, 80, 20, false),
			playlistsLoadedMsg{playlists: []spotify.Playlist{{ID: "p1", Name: "Road"}}, pageSize: 1, hasMore: true}},
		{"podcasts", newPodcastView(ctx, client, 80, 20, false),
			podcastsLoadedMsg{shows: []spotify.Show{{ID: "s1", Name: "Road"}}, hasMore: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.v.SearchableList().searching = true
			if cmd := tc.v.Update(tc.loaded); cmd == nil {
				t.Error("a page loaded during an active filter with more pages left must fetch the next page")
			}
		})
	}
}
