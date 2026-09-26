package lyrics

import (
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
