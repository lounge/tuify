package ui

import (
	"bytes"
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/theme"
	zone "github.com/lrstanley/bubblezone"
)

const homeTabWidth = 20

// Package-level style vars. They are populated by RebuildStyles, which
// must run after theme.Apply has reassigned the palette — lipgloss
// captures color values at style construction time, so styles built
// before Apply would render with stale defaults.
var (
	// Shared list item styles
	selectedStyle lipgloss.Style
	normalStyle   lipgloss.Style

	// Breadcrumb
	breadcrumbStyle lipgloss.Style

	// Now-playing bar
	nowPlayingTrackStyle  lipgloss.Style
	nowPlayingArtistStyle lipgloss.Style
	nowPlayingIconStyle   lipgloss.Style
	progressEmptyStyle    lipgloss.Style
	progressTimeStyle     lipgloss.Style

	// Home tabs
	homeTabActive   lipgloss.Style
	homeTabInactive lipgloss.Style

	// Shared
	errorStyle         lipgloss.Style
	loadingStyle       lipgloss.Style
	searchInputStyle   lipgloss.Style
	searchPrefixStyle  lipgloss.Style
	overlayBoxStyle    lipgloss.Style
	helpCmdStyle       lipgloss.Style
	helpDescStyle      lipgloss.Style
	searchHintBoxStyle lipgloss.Style
	helpOverlayStyle   lipgloss.Style
)

// RebuildStyles (re)constructs every package-level style from the current
// theme palette. Call once at startup after theme.Apply, before any UI
// rendering begins. Safe to call again if the theme is ever changed at
// runtime.
func RebuildStyles() {
	selectedStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(theme.Primary).
		Foreground(theme.Primary).
		Padding(0, 0, 0, 1)

	normalStyle = lipgloss.NewStyle().
		Padding(0, 0, 0, 2)

	breadcrumbStyle = lipgloss.NewStyle().
		Foreground(theme.Muted).
		MarginLeft(2).
		MarginBottom(1)

	nowPlayingTrackStyle = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true)

	nowPlayingArtistStyle = lipgloss.NewStyle().
		Foreground(theme.Text)

	nowPlayingIconStyle = lipgloss.NewStyle().
		Foreground(theme.Secondary)

	progressEmptyStyle = lipgloss.NewStyle().Foreground(theme.Dim)
	progressTimeStyle = lipgloss.NewStyle().Foreground(theme.Subtle)

	homeTabActive = lipgloss.NewStyle().
		Background(theme.Primary).
		Foreground(theme.OnPrimary).
		Width(homeTabWidth).
		Align(lipgloss.Center).
		Padding(1, 3)

	homeTabInactive = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Width(homeTabWidth).
		Align(lipgloss.Center).
		Padding(1, 3)

	errorStyle = lipgloss.NewStyle().
		Foreground(theme.Error)

	loadingStyle = lipgloss.NewStyle().
		Foreground(theme.Subtle)

	searchInputStyle = lipgloss.NewStyle().
		Foreground(theme.Secondary)

	searchPrefixStyle = lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true)

	overlayBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Secondary).
		Foreground(theme.Subtle)

	helpCmdStyle = lipgloss.NewStyle().Foreground(theme.Text)
	helpDescStyle = lipgloss.NewStyle().Foreground(theme.Subtle)

	searchHintBoxStyle = overlayBoxStyle.Padding(1, 2)
	helpOverlayStyle = overlayBoxStyle.Padding(1, 3)

	rebuildSpinnerStyle()
}

// init seeds the styles with the default palette so anything that reads
// them before bootstrap (e.g. tests that don't go through Run) still gets
// usable values. bootstrap calls RebuildStyles again after theme.Apply.
func init() {
	RebuildStyles()
}

func newListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.NormalTitle = normalStyle.Foreground(theme.Text)
	d.Styles.NormalDesc = d.Styles.NormalTitle.Foreground(theme.TextDim)
	d.Styles.SelectedTitle = selectedStyle
	d.Styles.SelectedDesc = selectedStyle.Foreground(theme.Subtle)
	d.Styles.DimmedTitle = normalStyle.Foreground(theme.TextDim)
	d.Styles.DimmedDesc = d.Styles.DimmedTitle.Foreground(theme.Dim)
	return d
}

// zoneListDelegate wraps the default delegate so each uriItem row is
// rendered inside a bubblezone Mark with the item's URI as the zone id.
// The main Update loop resolves mouse clicks to item indices by checking
// the URI-keyed zones against the click coordinates.
//
// Height(), Spacing(), and Update() are promoted from the embedded
// DefaultDelegate. If bubbles/list extends ItemDelegate in a future
// release, Go's compile-time check on list.New will flag the gap — but
// the silent inheritance is worth keeping aware of during upgrades.
type zoneListDelegate struct {
	list.DefaultDelegate
}

func (d zoneListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	u, ok := item.(uriItem)
	if !ok || u.URI() == "" {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	var buf bytes.Buffer
	d.DefaultDelegate.Render(&buf, m, index, item)
	fmt.Fprint(w, zone.Mark(u.URI(), buf.String()))
}
