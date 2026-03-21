package visualizers

import (
	"fmt"
	"math"
)

type Visualizer interface {
	Init(seed string, durationMs int)
	Advance()
	View(progressMs, width, height int) string
}

func xorshift(s uint64) uint64 {
	s ^= s << 13
	s ^= s >> 7
	s ^= s << 17
	return s
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func hslToRGB(h, s, l float64) (int, int, int) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	return clamp(int((r+m)*255), 0, 255),
		clamp(int((g+m)*255), 0, 255),
		clamp(int((b+m)*255), 0, 255)
}

// ansiColorStr returns a direct ANSI 24-bit foreground escape for the given color.
func ansiFg(r, g, b int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// ansiFgBg returns ANSI 24-bit foreground + background escapes.
func ansiFgBg(fr, fg, fb, br, bg, bb int) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm", fr, fg, fb, br, bg, bb)
}

const ansiReset = "\x1b[0m"
