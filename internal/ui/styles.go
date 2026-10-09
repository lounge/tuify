package ui

import (
	"bytes"
	"io"
	"strconv"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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
	progressSolidStyle    lipgloss.Style
	progressTipStyle      lipgloss.Style
	nowPlayingBoxStyle    lipgloss.Style
	// Gradient endpoints, parsed from the palette here rather than per frame.
	nowPlayingGradient adaptiveGradient
	progressGradient   adaptiveGradient

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
	deviceOverlayStyle lipgloss.Style
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
	progressSolidStyle = lipgloss.NewStyle().Foreground(theme.Primary)
	progressTipStyle = lipgloss.NewStyle().Foreground(theme.Tip)
	// Kept in sync with nowPlayingPadding.
	nowPlayingBoxStyle = lipgloss.NewStyle().Padding(0, 1)
	nowPlayingGradient = newAdaptiveGradient(theme.GradientStart, theme.GradientEnd)
	progressGradient = newAdaptiveGradient(theme.Primary, theme.Tip)

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
	deviceOverlayStyle = overlayBoxStyle.Padding(1, 3)

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
// rendered inside a bubblezone Mark. The zone id is the owning list's id
// plus the row index (rowZoneID), not the item's URI: a playlist can hold
// the same track twice, and two rows sharing one id would leave one of
// them unclickable and resolve the other to the wrong index. clickRow
// walks the visible rows with the same ids to resolve a click.
//
// Height(), Spacing(), and Update() are promoted from the embedded
// DefaultDelegate. If bubbles/list extends ItemDelegate in a future
// release, Go's compile-time check on list.New will flag the gap — but
// the silent inheritance is worth keeping aware of during upgrades.
type zoneListDelegate struct {
	list.DefaultDelegate
	listID uint64
}

func (d zoneListDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	u, ok := item.(uriItem)
	if !ok || u.URI() == "" {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	var buf bytes.Buffer
	d.DefaultDelegate.Render(&buf, m, index, item)
	_, _ = io.WriteString(w, zone.Mark(rowZoneID(d.listID, index), buf.String()))
}

// rowZoneID is the bubblezone id of the row at index in the list with the
// given id. List ids come from newFetchID, so rows of different lists
// never share an id even when they show the same item.
func rowZoneID(listID uint64, index int) string {
	return strconv.FormatUint(listID, 10) + ":" + strconv.Itoa(index)
}

// clickRow resolves a left-click against the zone-marked rows of l, whose
// rows zoneListDelegate marked with listID. It selects the row under the
// pointer and returns its zone id; empty return means the click missed
// every row. Shared by the clickAt implementations of the list screens.
func clickRow(l *list.Model, listID uint64, msg tea.MouseMsg) string {
	for i, item := range l.Items() {
		u, ok := item.(uriItem)
		if !ok || u.URI() == "" {
			continue
		}
		id := rowZoneID(listID, i)
		if zone.Get(id).InBounds(msg) {
			l.Select(i)
			return id
		}
	}
	return ""
}
