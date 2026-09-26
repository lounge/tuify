package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestWriteWithBackground_MatchesRegexp pins the hand-rolled SGR scanner
// to the regexp it replaced: bgEsc lands after every ESC [ [0-9;]* m and
// nowhere else, including after non-SGR escapes and truncated sequences.
func TestWriteWithBackground_MatchesRegexp(t *testing.T) {
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	const bg = "<BG>"
	inputs := []string{
		"",
		"plain",
		"\x1b[1mbold\x1b[0m",
		"a\x1b[mb\x1b[38;2;1;2;3mc",
		"\x1b[31;1m\x1b[0m",
		"cursor\x1b[2Kline\x1b[5A",
		"truncated\x1b[12;",
		"lone\x1b",
		"\x1b\x1b[1m",
		"zone\x1b[1234zmark\x1b[0m",
		"wide ▀ ● ━ \x1b[1m✓",
	}
	for _, in := range inputs {
		var b strings.Builder
		writeWithBackground(&b, in, bg)
		if want := sgr.ReplaceAllString(in, "${0}"+bg); b.String() != want {
			t.Errorf("writeWithBackground(%q) = %q, want %q", in, b.String(), want)
		}
	}
}

// renderTrackLine uses display-cell widths so wide runes (CJK, emoji)
// that count as 1 Unicode point but 2 cells don't slip past the budget
// and wrap the now-playing bar. A wrap would shift zone coordinates in
// the list above and make mouse clicks land on the wrong row.
func TestRenderTrackLine_WideRunesStaySingleLine(t *testing.T) {
	np := &nowPlayingModel{
		width:    40,
		hasTrack: true,
		track:    "あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほ", // ~60 cells of wide runes
		artist:   "まみむめも",
		playing:  true,
	}
	line := np.renderTrackLine()
	if strings.Contains(line, "\n") {
		t.Errorf("renderTrackLine produced multi-line output: %q", line)
	}
	// The rendered line should fit within the width budget once styling
	// is applied. lipgloss.Width ignores ANSI codes, so it's the cell count.
	if w := lipgloss.Width(line); w > np.width-nowPlayingPadding {
		t.Errorf("line width %d exceeds budget %d", w, np.width-nowPlayingPadding)
	}
}
