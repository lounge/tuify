package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View rendering: the main View() dispatcher, the help overlay, and the
// compact mini-mode layout. Kept separate from state/event handling so the
// rendering pass is easy to read as a unit.

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	if m.showDeviceSelector {
		return m.deviceSelector.view(m.width, m.height)
	}

	if m.miniMode {
		return m.miniModeView()
	}

	var b strings.Builder
	contentHeight := m.height - nowPlayingHeight

	if m.showHelp {
		b.WriteString(m.helpView(contentHeight))
	} else if m.visualizer.active {
		b.WriteString(m.visualizer.View(m.width, contentHeight))
	} else {
		if crumbs := m.currentView().Breadcrumb(); crumbs != "" {
			b.WriteString(breadcrumbStyle.Render(crumbs))
			b.WriteString("\n")
		}
		b.WriteString(m.currentView().View())
	}

	// Now playing bar
	b.WriteString("\n")
	var searchActive bool
	var searchQuery string
	if sv, ok := m.currentView().(*searchView); ok && sv.searching {
		searchActive = true
		searchQuery = sv.searchQuery
	} else if sl := m.searchableList(); sl != nil && sl.searching {
		searchActive = true
		searchQuery = sl.searchQuery
	}
	b.WriteString(m.nowPlaying.View(searchActive, searchQuery))

	return b.String()
}

func (m Model) helpView(height int) string {
	type entry struct{ cmd, desc string }
	var entries []entry
	if m.vimMode {
		entries = []entry{
			{"h / l", "navigate"},
			{"ctrl+d / u", "half page"},
			{", / .", "seek 5s"},
			{"space", "play / pause"},
			{"n / p", "next / prev"},
			{"r", "shuffle"},
			{"s", "stop"},
			{"c", "copy link"},
			{"v", "visualizer"},
			{"left / right", "cycle viz"},
			{"/", "search"},
			{"tab", "devices"},
			{"m", "mini mode"},
			{"?", "close help"},
			{"q", "quit"},
		}
	} else {
		entries = []entry{
			{"enter", "select"},
			{"esc", "back"},
			{"a / d", "seek 5s"},
			{"space", "play / pause"},
			{"n / p", "next / prev"},
			{"r", "shuffle"},
			{"s", "stop"},
			{"c", "copy link"},
			{"v", "visualizer"},
			{"left / right", "cycle viz"},
			{"/", "search"},
			{"tab", "devices"},
			{"m", "mini mode"},
			{"h", "close help"},
			{"q", "quit"},
		}
	}
	const cmdWidth = 13
	lines := make([]string, len(entries))
	for i, e := range entries {
		styled := helpCmdStyle.Render(e.cmd)
		pad := cmdWidth - lipgloss.Width(styled)
		if pad < 1 {
			pad = 1
		}
		lines[i] = styled + strings.Repeat(" ", pad) + helpDescStyle.Render(e.desc)
	}
	box := helpOverlayStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) miniModeView() string {
	np := m.nowPlaying
	if np.statusMsg != "" {
		style := lipgloss.NewStyle().Foreground(colorText)
		if np.statusIsError {
			style = errorStyle
		}
		return np.renderGradient([]string{style.Render(np.statusMsg)})
	}
	if !np.hasTrack {
		return np.renderGradient([]string{nowPlayingArtistStyle.Render("No track playing")})
	}

	icon := "⏸"
	if np.playing {
		icon = "▶"
	}
	iconStr := nowPlayingIconStyle.Render(icon)

	cur := formatDuration(time.Duration(np.progressMs) * time.Millisecond)
	total := formatDuration(time.Duration(np.durationMs) * time.Millisecond)
	timestamps := progressTimeStyle.Render(cur + "/" + total)

	iconLen := lipgloss.Width(iconStr)
	tsLen := lipgloss.Width(timestamps)
	innerWidth := m.width - nowPlayingPadding

	// Track — Artist label, truncated to fit.
	track := np.track
	artist := " — " + np.artist
	labelBudget := innerWidth - iconLen - tsLen - 8 // 4 spaces + 4 min bar
	if labelBudget < 1 {
		labelBudget = 1
	}
	labelRunes := []rune(track + artist)
	if len(labelRunes) > labelBudget {
		truncated := string(labelRunes[:labelBudget-1]) + "…"
		trackRunes := []rune(track)
		if labelBudget-1 <= len(trackRunes) {
			track = string(trackRunes[:labelBudget-1]) + "…"
			artist = ""
		} else {
			artist = string([]rune(truncated)[len(trackRunes):])
		}
	}
	var labelStr string
	if artist != "" {
		labelStr = nowPlayingTrackStyle.Render(track) + nowPlayingArtistStyle.Render(artist)
	} else {
		labelStr = nowPlayingTrackStyle.Render(track)
	}
	labelLen := lipgloss.Width(labelStr)

	// Progress bar fills remaining space.
	barWidth := innerWidth - iconLen - labelLen - tsLen - 4
	var barStr string
	if barWidth >= 4 {
		barStr = renderMiniBar(barWidth, np.progressMs, np.durationMs)
	}

	var line string
	if barStr != "" {
		line = iconStr + " " + labelStr + "  " + barStr + " " + timestamps
	} else {
		line = iconStr + " " + labelStr + "  " + timestamps
	}

	return np.renderGradient([]string{line})
}
