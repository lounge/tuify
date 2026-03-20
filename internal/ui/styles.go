package ui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Color palette — adaptive for light and dark terminals.
var (
	colorPrimary   = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7ee068"}
	colorSecondary = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#b48eff"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#626262"}
	colorSubtle    = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#8a8a8a"}
	colorDim       = lipgloss.AdaptiveColor{Light: "#BCBCBC", Dark: "#444444"}
	colorError     = lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#ff0087"}
	colorText      = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"}
	colorTextDim   = lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"}
	colorTip       = lipgloss.AdaptiveColor{Light: "#D4A017", Dark: "#FFD866"}
)

// Shared list item styles
var (
	selectedStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorPrimary).
			Foreground(colorPrimary).
			Padding(0, 0, 0, 1)

	normalStyle = lipgloss.NewStyle().
			Padding(0, 0, 0, 2)
)

// Breadcrumb
var breadcrumbStyle = lipgloss.NewStyle().
	Foreground(colorMuted).
	MarginLeft(2).
	MarginBottom(1)

// Now-playing bar
var (
	nowPlayingStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(colorSecondary).
			Padding(0, 1)

	nowPlayingTrackStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	nowPlayingArtistStyle = lipgloss.NewStyle().
				Foreground(colorText)

	nowPlayingIconStyle = lipgloss.NewStyle().
				Foreground(colorSecondary)

	progressEmptyStyle = lipgloss.NewStyle().Foreground(colorDim)
	progressTimeStyle   = lipgloss.NewStyle().Foreground(colorSubtle)
)

// Home tabs
var (
	homeTabWidth = 20

	homeTabActive = lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(lipgloss.Color("#000000")).
			Width(homeTabWidth).
			Align(lipgloss.Center).
			Padding(1, 3)

	homeTabInactive = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Width(homeTabWidth).
			Align(lipgloss.Center).
			Padding(1, 3)
)

// Shared
var (
	errorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)

func newListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.NormalTitle = normalStyle.Foreground(colorText)
	d.Styles.NormalDesc = d.Styles.NormalTitle.Foreground(colorTextDim)
	d.Styles.SelectedTitle = selectedStyle
	d.Styles.SelectedDesc = selectedStyle.Foreground(colorSubtle)
	d.Styles.DimmedTitle = normalStyle.Foreground(colorTextDim)
	d.Styles.DimmedDesc = d.Styles.DimmedTitle.Foreground(colorDim)
	return d
}
