package termsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format characters that stay: emoji sequences join their parts with the
// zero-width joiner, and Arabic, Persian and Indic scripts need the
// non-joiner to break ligatures inside a word.
const (
	zeroWidthNonJoiner = '\u200C'
	zeroWidthJoiner    = '\u200D'
)

// Clean removes Unicode control characters (C0, DEL and C1, which include
// ESC and the 8-bit CSI/OSC introducers) and format characters from s.
// Tab, newline and carriage return become a space, because every caller
// renders the result on one line. Invalid UTF-8 is replaced with U+FFFD,
// since a stray 0x9b byte is itself a CSI to a terminal in 8-bit mode.
// Clean input is returned unchanged and without allocating.
//
// Format characters (category Cf) execute nothing, but they are invisible
// and change how what surrounds them reads: a right-to-left override
// (U+202E) shows "evil<RLO>txt.3pm" as "evilmp3.txt", and a zero-width
// space makes two names the eye cannot tell apart. All are dropped except
// the zero-width joiner and non-joiner.
func Clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if strings.IndexFunc(s, unsafeRune) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unsafeRune(r):
			return -1
		}
		return r
	}, s)
}

// unsafeRune reports whether r must not reach the terminal: a control
// character, or a format character other than the two joiners.
func unsafeRune(r rune) bool {
	if unicode.IsControl(r) {
		return true
	}
	return r != zeroWidthJoiner && r != zeroWidthNonJoiner && unicode.Is(unicode.Cf, r)
}
