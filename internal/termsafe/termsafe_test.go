package termsafe

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestClean(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{"plain", "Bohemian Rhapsody", "Bohemian Rhapsody"},
		{"unicode", "Sigur Rós — Hoppípolla ♫", "Sigur Rós — Hoppípolla ♫"},
		{"osc 52 clipboard write", "evil\x1b]52;c;SGVsbG8=\x07name", "evil]52;c;SGVsbG8=name"},
		{"window title", "a\x1b]0;pwned\x07b", "a]0;pwnedb"},
		{"clear screen", "x\x1b[2Jy", "x[2Jy"},
		{"c1 csi", "a\u009b31mb", "a31mb"},
		{"del", "a\x7fb", "ab"},
		{"raw 8-bit csi byte", "a\x9b31mb", "a\uFFFD31mb"},
		{"whitespace controls", "line one\nline\ttwo\r", "line one line two "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Clean(tc.in); got != tc.want {
				t.Errorf("Clean(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClean_NoAllocWhenClean(t *testing.T) {
	s := "Nothing to strip here"
	if n := testing.AllocsPerRun(100, func() { _ = Clean(s) }); n != 0 {
		t.Errorf("Clean allocated %v times on a clean string", n)
	}
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{"", "plain", "\x1b]52;c;x\x07", "a\u009bb", "tab\tnl\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := Clean(s)
		if strings.IndexFunc(got, unicode.IsControl) >= 0 {
			t.Errorf("Clean(%q) = %q still contains a control character", s, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Clean(%q) = %q is not valid UTF-8", s, got)
		}
		if Clean(got) != got {
			t.Errorf("Clean is not idempotent on %q", s)
		}
	})
}
