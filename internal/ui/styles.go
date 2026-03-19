package ui

import "github.com/charmbracelet/lipgloss"

// Color palette — adaptive for light and dark terminals.
var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7ee068"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#626262"} // 241
	colorSubtle = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#8a8a8a"} // 245
	colorDim    = lipgloss.AdaptiveColor{Light: "#BCBCBC", Dark: "#444444"} // 238
	colorError  = lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#ff0087"} // 196

	colorSelected       = lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}
	colorSelectedBorder = lipgloss.AdaptiveColor{Light: "#F793FF", Dark: "#AD58B4"}
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
			BorderForeground(colorMuted).
			Padding(0, 1)

	nowPlayingTrackStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	nowPlayingArtistStyle = lipgloss.NewStyle().
				Foreground(colorSubtle)

	progressFilledStyle = lipgloss.NewStyle().Foreground(colorAccent)
	progressEmptyStyle  = lipgloss.NewStyle().Foreground(colorDim)
	progressTimeStyle   = lipgloss.NewStyle().Foreground(colorSubtle)
)

// Home menu
var (
	homeSelectedStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorSelectedBorder).
				Foreground(colorSelected).
				Padding(0, 0, 0, 1)

	homeNormalStyle = lipgloss.NewStyle().
			Padding(0, 0, 0, 2)

	homeMenuStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(1, 3)
)

// Shared
var (
	errorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
)
