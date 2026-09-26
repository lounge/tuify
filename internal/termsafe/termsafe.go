package termsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Clean removes Unicode control characters (C0, DEL and C1, which include
// ESC and the 8-bit CSI/OSC introducers) from s. Tab, newline and carriage
// return become a space, because every caller renders the result on one
// line. Invalid UTF-8 is replaced with U+FFFD, since a stray 0x9b byte is
// itself a CSI to a terminal in 8-bit mode. Clean input is returned
// unchanged and without allocating.
func Clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}
