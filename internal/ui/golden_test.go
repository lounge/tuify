package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/spotify"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// TestGoldenScreens pins full rendered frames, colour escapes included, for
// the main screens at a small and a large terminal. It is the safety net
// for render-path optimizations: a byte change in any frame fails it.
// After an intentional change, run: go test ./internal/ui -run Golden -update
// and review the diff of testdata/.
//
// Not parallel: it switches lipgloss's global colour profile.
func TestGoldenScreens(t *testing.T) {
	withTrueColor(t)
	prevDark := lipgloss.HasDarkBackground()
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() { lipgloss.SetHasDarkBackground(prevDark) })

	screens := []struct {
		name  string
		setup func(m *Model)
	}{
		{"home", func(*Model) {}},
		{"tracks", func(m *Model) {
			tv := newTrackView(m.rootCtx, m.client, "p1", "Roadtrip", 0, 0, false)
			m.pushView(tv)
			tv.Update(pageLoadedMsg{listID: tv.id, fetched: 3, items: []list.Item{
				trackItem{uri: "spotify:track:1", name: "Bohemian Rhapsody", artist: "Queen", album: "A Night at the Opera", duration: 354 * time.Second},
				trackItem{uri: "spotify:track:2", name: "Hoppípolla", artist: "Sigur Rós", album: "Takk...", duration: 268 * time.Second},
				trackItem{uri: "spotify:track:3", name: "Teardrop", artist: "Massive Attack", album: "Mezzanine", duration: 330 * time.Second},
			}})
		}},
		{"help", func(m *Model) { m.showHelp = true }},
		{"mini", func(m *Model) { m.miniMode = true }},
		{"devices", func(m *Model) {
			m.showDeviceSelector = true
			m.deviceSelector.handleLoaded(devicesLoadedMsg{devices: []spotify.Device{
				{ID: "d1", Name: "tuify", Type: "Computer", Active: true, Volume: 60},
				{ID: "d2", Name: "Living Room", Type: "Speaker", Volume: 30},
			}})
		}},
	}
	sizes := []struct{ w, h int }{{80, 24}, {120, 40}}

	for _, sc := range screens {
		for _, sz := range sizes {
			name := fmt.Sprintf("%s_%dx%d", sc.name, sz.w, sz.h)
			t.Run(name, func(t *testing.T) {
				m := newIntentTestModel()
				m.nowPlaying = benchNowPlaying()
				sc.setup(&m)
				updated, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
				got := updated.(Model).View()

				path := filepath.Join("testdata", name+".golden")
				if *update {
					if err := os.MkdirAll("testdata", 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run with -update to create it)", err)
				}
				if got != string(want) {
					t.Errorf("frame differs from %s\ngot (escapes shown):\n%s", path, strings.ReplaceAll(got, "\x1b", `\e`))
				}
			})
		}
	}
}
