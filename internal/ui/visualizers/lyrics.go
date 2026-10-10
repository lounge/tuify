package visualizers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Lyrics styles — fixed styles are pre-allocated, dynamic styles use ANSI escapes.
var (
	lyricsDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	lyricsHighlightDark  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true)
	lyricsHighlightLight = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Bold(true)
)

// lyricsLeadMs is how far ahead of the reported position the current line
// is chosen. The position arrives in whole-second steps and lags playback
// by up to a second plus poll latency, so looking slightly ahead lands the
// highlight on the line being sung rather than the one just finished.
const lyricsLeadMs = 500

// Lyrics shows the track's lyrics, or an "Instrumental" marker. With timed
// lines the line being sung is highlighted; untimed lines are scrolled in
// proportion to playback progress.
type Lyrics struct {
	lines        []LyricLine
	synced       bool  // every line carries a start time
	contentIdx   []int // indices of lines that are neither blank nor a [Section] marker
	durationMs   int
	progressMs   int
	loading      bool
	noLyrics     bool
	instrumental bool
	inited       bool
}

// NewLyrics returns a Lyrics with nothing loaded; call SetLyrics or
// SetInstrumental.
func NewLyrics() *Lyrics {
	return &Lyrics{}
}

func (l *Lyrics) Init(seed string, durationMs int) {
	l.durationMs = durationMs
	l.progressMs = 0
	l.lines = nil
	l.synced = false
	l.contentIdx = nil
	l.loading = true
	l.noLyrics = false
	l.instrumental = false
	l.inited = true
}

// SetLyrics installs the lines. They count as synced only when every line
// has a start time; a single untimed line puts the whole set on the
// proportional path.
func (l *Lyrics) SetLyrics(lines []LyricLine) {
	l.lines = lines
	l.loading = false
	l.noLyrics = len(lines) == 0
	l.instrumental = false
	l.synced = len(lines) > 0
	l.contentIdx = l.contentIdx[:0]
	for i, ln := range lines {
		if ln.StartMs < 0 {
			l.synced = false
		}
		if ln.Text != "" && !isSectionMarker(ln.Text) {
			l.contentIdx = append(l.contentIdx, i)
		}
	}
}

func (l *Lyrics) SetInstrumental() {
	l.lines = nil
	l.synced = false
	l.contentIdx = nil
	l.loading = false
	l.noLyrics = false
	l.instrumental = true
}

func (l *Lyrics) SetProgress(progressMs int) {
	l.progressMs = progressMs
}

func (l *Lyrics) Advance() {
}

// currentLine returns the index of the line to highlight, or -1 when
// playback has not reached the first timed line yet. Timed lines are
// looked up by position (plus lyricsLeadMs); the last line stays current
// past its stamp, and a stamped empty line is a valid current line, since
// it marks a break. Untimed lines fall back to the proportional estimate.
func (l *Lyrics) currentLine() int {
	if !l.synced {
		return l.proportionalLine()
	}
	target := l.progressMs + lyricsLeadMs
	return sort.Search(len(l.lines), func(i int) bool { return l.lines[i].StartMs > target }) - 1
}

// proportionalLine maps playback progress linearly onto the content lines,
// with blank lines and [Section] markers left out so intros and headers do
// not skew the estimate, then returns that line's index. Progress is
// clamped at both ends: it can run past the end while the poll lags, and a
// stale position can be negative.
func (l *Lyrics) proportionalLine() int {
	var progress float64
	if l.durationMs > 0 {
		progress = clampF64(float64(l.progressMs)/float64(l.durationMs), 0, 1)
	}
	if len(l.contentIdx) == 0 {
		return min(int(progress*float64(len(l.lines))), len(l.lines)-1)
	}
	return l.contentIdx[min(int(progress*float64(len(l.contentIdx))), len(l.contentIdx)-1)]
}

func (l *Lyrics) View(width, height int) string {
	if !l.inited || width < 1 || height < 1 {
		return ""
	}

	if l.loading {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lyricsDimStyle.Render("Loading lyrics..."))
	}
	if l.instrumental {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lyricsDimStyle.Render("Instrumental"))
	}
	if l.noLyrics {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			lyricsDimStyle.Render("No lyrics found"))
	}

	currentLine := l.currentLine()
	totalLines := len(l.lines)

	// Compute the visible window: keep the current line centered, or pin
	// to the top before the first timed line, and clamp so the window
	// never shows empty space at the edges.
	startLine := max(max(currentLine, 0)-height/2, 0)
	endLine := startLine + height
	if endLine > totalLines {
		endLine = totalLines
		startLine = max(endLine-height, 0)
	}

	// If lyrics fit in the viewport, center the block vertically.
	visibleLines := endLine - startLine
	topPad := 0
	if visibleLines < height {
		topPad = (height - visibleLines) / 2
	}

	isDark := lipgloss.HasDarkBackground()
	emptyRow := strings.Repeat(" ", width)

	var buf strings.Builder
	buf.Grow(width * height * 20)

	for row := range height {
		if row > 0 {
			buf.WriteRune('\n')
		}

		lineIdx := row - topPad + startLine
		if lineIdx < 0 || lineIdx < startLine || lineIdx >= endLine {
			buf.WriteString(emptyRow)
			continue
		}

		line := l.lines[lineIdx].Text

		// Truncate to width.
		runes := []rune(line)
		if len(runes) > width {
			runes = runes[:width]
			line = string(runes)
		}

		dist := lineIdx - currentLine
		if dist < 0 {
			dist = -dist
		}

		isSection := isSectionMarker(line)
		padded := centerPad(line, width)

		if lineIdx == currentLine {
			style := lyricsHighlightDark
			if !isDark {
				style = lyricsHighlightLight
			}
			buf.WriteString(style.Width(width).Render(padded))
		} else {
			g := lyricGray(isDark, isSection, dist)
			writeAnsiFg(&buf, g, g, g)
			buf.WriteString(padded)
			buf.WriteString(ansiReset)
		}
	}

	return buf.String()
}

// isSectionMarker reports whether a lyric line is a header such as
// "[Verse 1]" or "[Chorus]", as Genius text carries.
func isSectionMarker(line string) bool {
	return len(line) > 0 && line[0] == '['
}

// lyricGray returns a gray intensity (0–255) for a lyrics line based on
// dark/light mode, whether it's a section marker, and distance from current.
func lyricGray(isDark, isSection bool, dist int) int {
	if isDark {
		if isSection {
			return clamp(100-dist*12, 40, 255)
		}
		return clamp(180-dist*25, 50, 255)
	}
	if isSection {
		return clamp(100+dist*12, 0, 160)
	}
	return clamp(80+dist*25, 0, 200)
}

func centerPad(s string, width int) string {
	runes := []rune(s)
	n := len(runes)
	if n >= width {
		return s
	}
	left := (width - n) / 2
	return fmt.Sprintf("%*s%s", left, "", s)
}
