package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// ansiSGR matches any ANSI SGR (Select Graphic Rendition) escape sequence.
var ansiSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (m nowPlayingModel) progressBarView() string {
	return renderProgressBar(m.width, m.progressMs, m.durationMs)
}

func (m nowPlayingModel) View(searchActive bool, searchQuery string) string {
	if m.statusMsg != "" {
		lines := []string{"", renderStatusLine(m.statusMsg, m.statusSpinning, m.statusIsError), "", "", ""}
		if searchActive {
			lines = append(lines, "")
		}
		return m.renderGradient(lines)
	}

	var status string
	if m.hasTrack {
		icon := "⏸"
		if m.playing {
			icon = "▶"
		}
		shuffle := ""
		if m.shuffling {
			shuffle = "[shuffle] "
		}
		left := fmt.Sprintf("%s %s%s — %s",
			nowPlayingIconStyle.Render(icon),
			nowPlayingIconStyle.Render(shuffle),
			nowPlayingTrackStyle.Render(m.track),
			nowPlayingArtistStyle.Render(m.artist),
		)
		device := ""
		if m.deviceName != "" {
			device = nowPlayingTrackStyle.Render("◉ ") + nowPlayingArtistStyle.Render(m.deviceName)
		}
		if device != "" {
			leftLen := lipgloss.Width(left)
			deviceLen := lipgloss.Width(device)
			innerWidth := m.width - nowPlayingPadding
			gap := innerWidth - leftLen - deviceLen
			if gap >= 2 {
				status = left + strings.Repeat(" ", gap) + device
			} else {
				status = left
			}
		} else {
			status = left
		}
	} else {
		status = nowPlayingArtistStyle.Render("No track playing")
	}

	var progress string
	if m.hasTrack {
		progress = m.progressBarView()
	}

	lines := []string{"", status, "", progress, ""}
	if searchActive {
		var search string
		if idx := strings.Index(searchQuery, ":"); idx > 0 {
			pre := searchQuery[:idx+1]
			rest := searchQuery[idx+1:]
			search = searchPrefixStyle.Render("/"+pre) + searchInputStyle.Render(rest+"█")
		} else {
			search = searchInputStyle.Render("/" + searchQuery + "█")
		}
		lines = append(lines, search)
	}
	return m.renderGradient(lines)
}

// renderGradient renders the now-playing area with a purple background that
// fades from top to bottom.
func (m nowPlayingModel) renderGradient(lines []string) string {
	startC, _ := colorful.Hex(resolveHex(colorGradientStart))
	endC, _ := colorful.Hex(resolveHex(colorGradientEnd))

	// Render the entire block through lipgloss for correct width/wrapping,
	// then apply per-line gradient to the visual output.
	content := strings.Join(lines, "\n")
	rendered := lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(content)
	visualLines := strings.Split(rendered, "\n")

	var b strings.Builder
	total := len(visualLines)

	for i, vl := range visualLines {
		var t float64
		if total > 1 {
			t = float64(i) / float64(total-1)
		}
		c := startC.BlendLab(endC, t).Clamped()
		r, g, bl := c.RGB255()
		bgEsc := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, bl)
		vl = ansiSGR.ReplaceAllString(vl, "${0}"+bgEsc)
		b.WriteString(bgEsc + vl + "\x1b[0m")
		if i < total-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
