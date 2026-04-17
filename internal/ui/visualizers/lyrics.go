package visualizers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Lyrics styles — fixed styles are pre-allocated, dynamic styles use ANSI escapes.
var (
	lyricsDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	lyricsHighlightDark  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true)
	lyricsHighlightLight = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Bold(true)
)

type Lyrics struct {
	lines        []string
	durationMs   int
	progressMs   int
	loading      bool
	noLyrics     bool
	instrumental bool
	inited       bool
}

func NewLyrics() *Lyrics {
	return &Lyrics{}
}

func (l *Lyrics) Init(seed string, durationMs int) {
	l.durationMs = durationMs
	l.progressMs = 0
	l.lines = nil
	l.loading = true
	l.noLyrics = false
	l.instrumental = false
	l.inited = true
}

func (l *Lyrics) SetLyrics(lines []string) {
	l.lines = lines
	l.loading = false
	l.noLyrics = len(lines) == 0
	l.instrumental = false
}

func (l *Lyrics) SetInstrumental() {
	l.lines = nil
	l.loading = false
	l.noLyrics = false
	l.instrumental = true
}

func (l *Lyrics) SetProgress(progressMs int) {
	l.progressMs = progressMs
}

func (l *Lyrics) Advance() {
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

	// Estimate which line we're on based on playback progress.
	var progress float64
	if l.durationMs > 0 {
		progress = float64(l.progressMs) / float64(l.durationMs)
	}
	if progress > 1 {
		progress = 1
	}

	// Map progress to a line index, snapping to the nearest non-blank line.
	totalLines := len(l.lines)
	currentLine := int(progress * float64(totalLines))
	if currentLine >= totalLines {
		currentLine = totalLines - 1
	}
	if l.lines[currentLine] == "" {
		// Search forward then backward for a non-blank line.
		best := currentLine
		for i := currentLine + 1; i < totalLines; i++ {
			if l.lines[i] != "" {
				best = i
				break
			}
		}
		if best == currentLine {
			for i := currentLine - 1; i >= 0; i-- {
				if l.lines[i] != "" {
					best = i
					break
				}
			}
		}
		currentLine = best
	}

	// Compute the visible window of lyrics lines.
	// Scroll to keep currentLine centered, but clamp to avoid empty space at edges.
	startLine := currentLine - height/2
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine + height
	if endLine > totalLines {
		endLine = totalLines
		startLine = endLine - height
		if startLine < 0 {
			startLine = 0
		}
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

	for row := 0; row < height; row++ {
		if row > 0 {
			buf.WriteRune('\n')
		}

		lineIdx := row - topPad + startLine
		if lineIdx < 0 || lineIdx < startLine || lineIdx >= endLine {
			buf.WriteString(emptyRow)
			continue
		}

		line := l.lines[lineIdx]

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

		isSection := len(line) > 0 && line[0] == '['
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
