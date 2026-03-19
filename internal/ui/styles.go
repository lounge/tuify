package ui

import "github.com/charmbracelet/lipgloss"

var (
	breadcrumbStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginBottom(1)

	nowPlayingStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("241")).
			Padding(0, 1)

	nowPlayingTrackStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("40")).
				Bold(true)

	nowPlayingArtistStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	progressFilledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("40"))
	progressEmptyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	progressTimeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)
