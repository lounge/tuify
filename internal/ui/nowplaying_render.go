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
		labelBudget = innerWidth - prefixW
	}

	left := nowPlayingIconStyle.Render(icon) + " " +
		nowPlayingIconStyle.Render(shufflePrefix) +
		m.renderLabel(labelBudget)

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

// labelStreamWidth returns the display width of the full marquee stream
// (track + separator + artist + marqueeGap). Used by the label-scroll
// tick handler to bound labelScrollOffset within [0, streamW).
func (m nowPlayingModel) labelStreamWidth() int {
	return runewidth.StringWidth(m.track + " — " + m.artist + marqueeGap)
}

// renderLabel returns the styled "track — artist" label, fitted to
// budget display cells. Fits: two-tone static render. Doesn't fit:
// marquee-scrolls at the current labelScrollOffset with matching colors.
// Very small budgets: degrade to a single truncated track segment so the
// caller still sees something meaningful.
func (m nowPlayingModel) renderLabel(budget int) string {
	if budget <= 0 {
		return ""
	}
	sep := " — "
	label := m.track + sep + m.artist
	labelW := runewidth.StringWidth(label)
	switch {
	case labelW <= budget:
		return nowPlayingTrackStyle.Render(m.track) +
			nowPlayingArtistStyle.Render(sep+m.artist)
	case budget < 4:
		return nowPlayingTrackStyle.Render(runewidth.Truncate(m.track, budget, "…"))
	default:
		return marqueeWindow(m.track, sep+m.artist, m.labelScrollOffset, budget)
	}
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

// marqueeGap is appended to the tail before looping so the start of the
// next repeat is visually separated from the end of the current one.
const marqueeGap = "   •   "

// marqueeWindow returns a width-cell styled slice of the "track + tail"
// stream starting at the given cell offset. Precondition: width must be
// no larger than streamW (track + tail + marqueeGap), otherwise the
// doubled stream we slice from won't cover the requested window. Callers
// should gate this with a "doesn't fit" check (see renderLabel).
//
// Runes from the track segment are rendered with nowPlayingTrackStyle;
// everything else (tail + gap)
// with nowPlayingArtistStyle, matching the static two-tone layout. Wraps
// around so output is always exactly width cells.
//
// Styling is determined by the cell position where each rune begins; a
// multi-cell rune that straddles the track/tail boundary takes the style
// of its starting half. In practice the boundary is " — ", so this is
// only visible on CJK titles, and the flicker is imperceptible.
func marqueeWindow(track, tail string, offset, width int) string {
	if width <= 0 {
		return ""
	}
	stream := track + tail + marqueeGap
	streamW := runewidth.StringWidth(stream)
	if streamW == 0 {
		return strings.Repeat(" ", width)
	}
	trackW := runewidth.StringWidth(track)
	offset = ((offset % streamW) + streamW) % streamW

	// Visible slice of the doubled stream starting at offset, width cells.
	doubled := stream + stream
	visible := runewidth.Truncate(runewidth.TruncateLeft(doubled, offset, ""), width, "")

	var out strings.Builder
	var seg strings.Builder
	currentIsTrack := false
	cellInStream := offset % streamW
	haveSeg := false

	flush := func() {
		if !haveSeg {
			return
		}
		if currentIsTrack {
			out.WriteString(nowPlayingTrackStyle.Render(seg.String()))
		} else {
			out.WriteString(nowPlayingArtistStyle.Render(seg.String()))
		}
		seg.Reset()
		haveSeg = false
	}

	emitted := 0
	for _, r := range visible {
		rw := runewidth.RuneWidth(r)
		isTrack := cellInStream < trackW
		if haveSeg && isTrack != currentIsTrack {
			flush()
		}
		seg.WriteRune(r)
		currentIsTrack = isTrack
		haveSeg = true
		cellInStream = (cellInStream + rw) % streamW
		emitted += rw
	}
	flush()

	if emitted < width {
		out.WriteString(nowPlayingArtistStyle.Render(strings.Repeat(" ", width-emitted)))
	}
	return out.String()
}
