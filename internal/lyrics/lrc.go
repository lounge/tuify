package lyrics

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/lounge/tuify/internal/termsafe"
)

var (
	// reLRCStamp matches one line timestamp at the start of a string:
	// [mm:ss], [mm:ss.xx], [mm:ss.xxx], with ':' accepted as the fraction
	// separator too. Minutes may run to three digits for long tracks.
	reLRCStamp = regexp.MustCompile(`^\[(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?\]`)
	// reLRCWordStamp matches the <mm:ss.xx> word timestamps of enhanced
	// LRC, which carry no line information and would otherwise show up as
	// text.
	reLRCWordStamp = regexp.MustCompile(`<\d{1,3}:\d{2}(?:[.:]\d{1,3})?>`)
)

// parseLRC parses LRC text into timed Lines sorted by start time. A line
// with several timestamps becomes one Line per timestamp. Lines without
// a leading timestamp, which includes metadata tags such as [ar:...] and
// [offset:...], are dropped. A timestamp with no text is kept: it marks
// an instrumental break. Every text passes through termsafe.Clean.
func parseLRC(s string) []Line {
	var lines []Line
	for raw := range strings.SplitSeq(strings.TrimPrefix(s, "\ufeff"), "\n") {
		raw = strings.TrimSpace(raw)
		var stamps []int
		for {
			m := reLRCStamp.FindStringSubmatch(raw)
			if m == nil {
				break
			}
			stamps = append(stamps, stampMs(m[1], m[2], m[3]))
			raw = raw[len(m[0]):]
		}
		if len(stamps) == 0 {
			continue
		}
		text := cleanLRCText(raw)
		for _, ms := range stamps {
			lines = append(lines, Line{StartMs: ms, Text: text})
		}
	}
	slices.SortStableFunc(lines, func(a, b Line) int {
		return cmp.Compare(a.StartMs, b.StartMs)
	})
	return lines
}

// stampMs converts the captured minute, second and fraction fields of a
// timestamp to milliseconds. The fraction may be 1 to 3 digits, or empty.
func stampMs(mins, secs, frac string) int {
	m, _ := strconv.Atoi(mins)
	s, _ := strconv.Atoi(secs)
	ms := (m*60 + s) * 1000
	if frac != "" {
		f, _ := strconv.Atoi(frac)
		for range 3 - len(frac) {
			f *= 10
		}
		ms += f
	}
	return ms
}

// cleanLRCText strips control characters, then word timestamps, from the
// text that follows a line timestamp and trims it. Control characters go
// first so one hiding inside a word timestamp cannot keep it from being
// stripped.
func cleanLRCText(raw string) string {
	raw = termsafe.Clean(raw)
	if reLRCWordStamp.MatchString(raw) {
		raw = strings.Join(strings.Fields(reLRCWordStamp.ReplaceAllString(raw, " ")), " ")
	}
	return strings.TrimSpace(raw)
}
