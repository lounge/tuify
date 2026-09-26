package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// On-demand tick chains: the spinner and label marquee keep ticking only
// while something on screen uses them, and Update restarts them the
// moment the state needs them again.

func newTickerTestModel(t *testing.T) Model {
	t.Helper()
	np := newTestNowPlaying(t)
	np.width = 80
	return Model{
		nowPlaying: np,
		client:     &spotify.Client{},
		viewStack:  []view{newHomeView(80, 20)},
		width:      80,
		height:     24,
	}
}

func TestSpinnerTick_StopsWhenNothingSpins(t *testing.T) {
	m := newTickerTestModel(t)
	m.spinnerTicking = true

	updated, cmd := m.Update(spinner.TickMsg{ID: loadingSpinner.ID()})
	if cmd != nil {
		t.Error("an idle spinner tick must not reschedule")
	}
	if updated.(Model).spinnerTicking {
		t.Error("spinnerTicking should clear once the chain stops")
	}
}

func TestSpinnerTick_ContinuesWhileListLoads(t *testing.T) {
	m := newTickerTestModel(t)
	m.viewStack = append(m.viewStack, newPlaylistView(t.Context(), m.client, 80, 20, false))
	m.spinnerTicking = true

	updated, cmd := m.Update(spinner.TickMsg{ID: loadingSpinner.ID()})
	if cmd == nil {
		t.Fatal("a loading list must keep the spinner chain alive")
	}
	if !updated.(Model).spinnerTicking {
		t.Error("spinnerTicking should stay set while the chain runs")
	}
}

func TestNeedsSpinner(t *testing.T) {
	loadingMore := statusItem{text: "Loading more…", spinning: true}
	tests := []struct {
		name  string
		setup func(m *Model)
		want  bool
	}{
		{"idle home", func(*Model) {}, false},
		{"spinning status banner", func(m *Model) { m.nowPlaying.SetSpinningInfo("Switching") }, true},
		{"device overlay loading", func(m *Model) { m.showDeviceSelector = true; m.deviceSelector.loading = true }, true},
		{"device loading but overlay closed", func(m *Model) { m.deviceSelector.loading = true }, false},
		{"trailing loading-more row", func(m *Model) {
			pv := newPlaylistView(t.Context(), m.client, 80, 20, false)
			pv.list.SetItems([]list.Item{playlistItem{id: "p1", name: "Road"}, loadingMore})
			m.viewStack = append(m.viewStack, pv)
		}, true},
		{"loaded list", func(m *Model) {
			pv := newPlaylistView(t.Context(), m.client, 80, 20, false)
			pv.list.SetItems([]list.Item{playlistItem{id: "p1", name: "Road"}})
			m.viewStack = append(m.viewStack, pv)
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTickerTestModel(t)
			tc.setup(&m)
			if got := m.needsSpinner(); got != tc.want {
				t.Errorf("needsSpinner() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpdate_RestartsSpinnerOnce(t *testing.T) {
	m := newTickerTestModel(t)

	// A device transfer shows a spinning "Switching to…" banner; the
	// Update that sets it must also restart the idle spinner.
	updated, cmd := m.Update(transferDeviceMsg{deviceName: "Kitchen"})
	m = updated.(Model)
	if !m.spinnerTicking || cmd == nil {
		t.Fatal("Update should restart the spinner when a spinning banner appears")
	}

	// With the chain running, nothing may start a second one.
	if cmd := m.resumeTickers(); cmd != nil {
		t.Error("resumeTickers must not start a spinner that is already ticking")
	}
}

func TestLabelScroll_RunsOnlyWhileLabelOverflows(t *testing.T) {
	m := newTickerTestModel(t)
	m.nowPlaying.hasTrack = true
	m.nowPlaying.track = strings.Repeat("Long Track Title ", 6)
	m.nowPlaying.artist = "Artist"

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	if !m.nowPlaying.labelTicking {
		t.Fatal("an overflowing label should start the marquee tick")
	}

	updated, cmd := m.Update(labelScrollMsg{})
	m = updated.(Model)
	if cmd == nil || m.nowPlaying.labelScrollOffset != 1 {
		t.Fatalf("overflowing label tick: offset=%d cmd=%v, want offset 1 and a rescheduled tick",
			m.nowPlaying.labelScrollOffset, cmd != nil)
	}

	// Shrink the label so it fits: the next tick stops the chain and
	// resets the offset.
	m.nowPlaying.track = "Short"
	updated, cmd = m.Update(labelScrollMsg{})
	m = updated.(Model)
	if cmd != nil || m.nowPlaying.labelTicking || m.nowPlaying.labelScrollOffset != 0 {
		t.Errorf("fitting label tick: cmd=%v ticking=%v offset=%d, want stopped at 0",
			cmd != nil, m.nowPlaying.labelTicking, m.nowPlaying.labelScrollOffset)
	}
}

func TestLabelNeedsScroll_UsesMiniModeBudget(t *testing.T) {
	m := newTickerTestModel(t)
	m.nowPlaying.hasTrack = true
	m.nowPlaying.track = "Track"
	m.nowPlaying.artist = strings.Repeat("a", 60)

	// 80 wide: fits the full track line but not mini mode, which also
	// reserves room for timestamps and a minimum progress bar.
	if m.labelNeedsScroll() {
		t.Fatal("label should fit the full-width track line")
	}
	m.miniMode = true
	if !m.labelNeedsScroll() {
		t.Error("label should overflow the mini-mode budget")
	}
}
