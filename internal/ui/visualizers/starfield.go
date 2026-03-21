package visualizers

import (
	"hash/fnv"
	"math"
	"strings"
)

const numStars = 200

var starChars = [5]rune{'·', '∙', '•', '⦁', '★'}

type star struct {
	x, y, z float64
	speed   float64
}

type Starfield struct {
	stars  []star
	rng    uint64
	inited bool
	grid   []rune
	colors []int32 // packed RGB (-1 = no star)
	gridW  int
	gridH  int
}

func NewStarfield() *Starfield {
	return &Starfield{}
}

func (sf *Starfield) Init(seed string, durationMs int) {
	h := fnv.New64a()
	h.Write([]byte(seed))
	sf.rng = h.Sum64()
	sf.stars = make([]star, numStars)
	for i := range sf.stars {
		sf.stars[i] = sf.newStar(true)
	}
	sf.inited = true
}

func (sf *Starfield) Advance() {
	if !sf.inited {
		return
	}
	dt := 0.015
	for i := range sf.stars {
		s := &sf.stars[i]
		s.z -= s.speed * dt
		if s.z <= 0.01 {
			sf.stars[i] = sf.newStar(false)
		}
	}
}

func (sf *Starfield) View(progressMs, width, height int) string {
	if !sf.inited || width < 1 || height < 1 {
		return ""
	}

	// Resize grid buffers if needed.
	size := width * height
	if sf.gridW != width || sf.gridH != height {
		sf.grid = make([]rune, size)
		sf.colors = make([]int32, size)
		sf.gridW = width
		sf.gridH = height
	}

	// Clear grid.
	for i := range size {
		sf.grid[i] = ' '
		sf.colors[i] = -1
	}

	halfW := float64(width) / 2
	halfH := float64(height) / 2

	for _, s := range sf.stars {
		px := s.x/s.z*halfW + halfW
		py := s.y/s.z*halfH + halfH

		col := int(px)
		row := int(py)
		if col < 0 || col >= width || row < 0 || row >= height {
			continue
		}

		closeness := 1.0 - s.z
		charIdx := int(closeness * float64(len(starChars)-1))
		if charIdx < 0 {
			charIdx = 0
		}
		if charIdx >= len(starChars) {
			charIdx = len(starChars) - 1
		}

		lum := 0.3 + closeness*0.7
		hue := math.Mod(s.x*180+s.y*180+200, 360)
		if hue < 0 {
			hue += 360
		}
		sat := 0.1 + closeness*0.3

		r, g, b := hslToRGB(hue, sat, lum)
		idx := row*width + col
		sf.grid[idx] = starChars[charIdx]
		sf.colors[idx] = int32(r)<<16 | int32(g)<<8 | int32(b)
	}

	var buf strings.Builder
	buf.Grow(size * 4)

	for r := range height {
		for c := range width {
			idx := r*width + c
			if sf.colors[idx] < 0 {
				buf.WriteRune(' ')
			} else {
				cr := int(sf.colors[idx] >> 16 & 0xFF)
				cg := int(sf.colors[idx] >> 8 & 0xFF)
				cb := int(sf.colors[idx] & 0xFF)
				buf.WriteString(ansiFg(cr, cg, cb))
				buf.WriteRune(sf.grid[idx])
				buf.WriteString(ansiReset)
			}
		}
		if r < height-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}

func (sf *Starfield) newStar(randomDepth bool) star {
	sf.rng = xorshift(sf.rng)
	x := float64(sf.rng%2000)/1000.0 - 1.0
	sf.rng = xorshift(sf.rng)
	y := float64(sf.rng%2000)/1000.0 - 1.0
	sf.rng = xorshift(sf.rng)
	speed := 0.3 + float64(sf.rng%700)/1000.0

	z := 1.0
	if randomDepth {
		sf.rng = xorshift(sf.rng)
		z = float64(sf.rng%1000) / 1000.0
	}
	return star{x: x, y: y, z: z, speed: speed}
}
