package lyrics

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzExtractLyrics pins the shape of text that reaches the lyrics
// visualizer, whatever HTML Genius (or someone editing lyrics on it) serves:
// no terminal control bytes, blank-line runs collapsed, no leading or
// trailing blank line, and normalizeLyrics being a fixed point on it.
func FuzzExtractLyrics(f *testing.F) {
	for _, seed := range []string{
		`<html><body>
		<div data-lyrics-container="true">Hello<br>World</div>
		<div>not lyrics</div>
		<div data-lyrics-container="true">Second verse</div>
	</body></html>`,
		`<div data-lyrics-container="true">
			Keep this
			<span data-exclude-from-selection="true">Remove this</span>
		</div>`,
		`<div data-lyrics-container="true">[Verse 1]<br>Line one<br><br><br>Line two<br>[Chorus]<br><i>La</i> la</div>`,
		`<div data-lyrics-container="true">a&#27;]52;c;SGVsbG8=&#7;b<br>raw ` + "\x1b[2J" + `line</div>`,
		`<div data-lyrics-container="true"> <br>&#13;&#133;<br>[x]<br>  [y]  </div><div data-lyrics-container="true"><br></div>`,
		`<html><body><div>no lyrics here</div></body></html>`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, page string) {
		got, err := extractLyrics(strings.NewReader(page))
		if err != nil {
			return
		}
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 in output %q", got)
		}
		if i := strings.IndexFunc(got, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }); i >= 0 {
			t.Fatalf("control character %U at byte %d in %q", []rune(got[i:])[0], i, got)
		}
		if strings.Contains(got, "\n\n\n") {
			t.Fatalf("triple newline in %q", got)
		}
		if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
			t.Fatalf("leading or trailing blank line in %q", got)
		}
		if again := normalizeLyrics(got); again != got {
			t.Fatalf("normalizeLyrics not idempotent:\n first: %q\nsecond: %q", got, again)
		}
	})
}

// FuzzParseLRC pins the shape of the timed lines the lyrics visualizer
// receives, whatever LRC text LRCLIB (or someone uploading to it) serves:
// non-negative timestamps in non-decreasing order, clean trimmed text with
// no control runes, and a round trip through the canonical
// "[mm:ss.xxx] text" form that yields the same lines.
func FuzzParseLRC(f *testing.F) {
	for _, seed := range []string{
		"[00:00.15] Is this the real life?\n[00:07.13] Caught in a landslide\n[02:35.66]\n",
		"[ar:Queen]\n[ti:Song]\n[offset:+500]\n[00:01.00][00:03.00] again\n",
		"\xef\xbb\xbf[00:01.00] a\r\n[00:02] b\r\n[00:03.5] c",
		"[00:01.00] <00:01.00>Hi <00:01.50>there\n",
		"[999:59.999] end",
		"no stamps at all\n\n",
		"[00:01.00] a\x1b]52;c;SGVsbG8=\x07b\t[00:02.00]",
		"[00:01.00] [00:02.00] x",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		lines := parseLRC(in)
		for i, l := range lines {
			if l.StartMs < 0 {
				t.Fatalf("negative StartMs %d in %+v", l.StartMs, l)
			}
			if i > 0 && lines[i-1].StartMs > l.StartMs {
				t.Fatalf("lines out of order at %d: %v", i, lines)
			}
			if !utf8.ValidString(l.Text) {
				t.Fatalf("invalid UTF-8 in %q", l.Text)
			}
			if j := strings.IndexFunc(l.Text, unicode.IsControl); j >= 0 {
				t.Fatalf("control character %U at byte %d in %q", []rune(l.Text[j:])[0], j, l.Text)
			}
			if strings.TrimSpace(l.Text) != l.Text {
				t.Fatalf("untrimmed text %q", l.Text)
			}
		}
		var sb strings.Builder
		for _, l := range lines {
			fmt.Fprintf(&sb, "[%02d:%02d.%03d] %s\n", l.StartMs/60000, l.StartMs/1000%60, l.StartMs%1000, l.Text)
		}
		if again := parseLRC(sb.String()); !slices.Equal(again, lines) {
			t.Fatalf("round trip differs:\n first: %v\nsecond: %v", lines, again)
		}
	})
}
