package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
	runewidth "github.com/mattn/go-runewidth"
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
		status = m.renderTrackLine()
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

// renderTrackLine builds the one-line "icon track — artist   device" status,
// truncating the label portion so the rendered line stays within m.width.
// Keeping it on a single line is load-bearing: a wrapped NP bar makes the
// total View() output exceed terminal height, which scrolls the viewport
// and shifts mouse-click zone coordinates in the list above.
func (m nowPlayingModel) renderTrackLine() string {
	icon := "⏸"
	if m.playing {
		icon = "▶"
	}
	shufflePrefix := ""
	if m.shuffling {
		shufflePrefix = "[shuffle] "
	}
	devicePlain := ""
	if m.deviceName != "" {
		devicePlain = "◉ " + m.deviceName
	}

	innerWidth := m.width - nowPlayingPadding
	// Fixed portion consumed before the label: icon + space + shuffle prefix.
	prefixW := lipgloss.Width(icon) + 1 + lipgloss.Width(shufflePrefix)
	// Trailing: 2-space gap + device, dropped if it wouldn't leave room.
	trailingW := 0
	if devicePlain != "" {
		trailingW = 2 + lipgloss.Width(devicePlain)
	}
	labelBudget := innerWidth - prefixW - trailingW
	if labelBudget < 8 {
		// Not enough room; drop the device and use all remaining width
		// for the track/artist label.
		devicePlain = ""
		trailingW = 0
		labelBudget = innerWidth - prefixW
	}

	track, artist, sep := m.track, m.artist, " — "
	// Use display-cell widths (not rune counts) so wide runes — CJK,
	// emoji, etc. — don't slip past the budget and wrap the line.
	if runewidth.StringWidth(track+sep+artist) > labelBudget && labelBudget > 1 {
		trackSepW := runewidth.StringWidth(track + sep)
		if trackSepW >= labelBudget {
			// Label bigger than budget even without artist — truncate track.
			track = runewidth.Truncate(track, labelBudget, "…")
			artist, sep = "", ""
		} else {
			artistBudget := labelBudget - trackSepW
			if artistBudget > 1 {
				artist = runewidth.Truncate(artist, artistBudget, "…")
			} else {
				artist, sep = "", ""
			}
		}
	}

	left := nowPlayingIconStyle.Render(icon) + " " +
		nowPlayingIconStyle.Render(shufflePrefix) +
		nowPlayingTrackStyle.Render(track)
	if artist != "" {
		left += nowPlayingArtistStyle.Render(sep + artist)
	}

	if devicePlain == "" {
		return left
	}
	device := nowPlayingTrackStyle.Render("◉ ") + nowPlayingArtistStyle.Render(m.deviceName)
	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(device)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + device
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
