package ui

import (
	"regexp"
	"strings"
	"testing"
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
