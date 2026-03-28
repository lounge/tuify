package visualizers

import (
	"hash/fnv"
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

const (
	baseStars = 350
	maxStars  = 900
)

var starChars = [5]rune{'∙', '•', '⦁', '★', '✦'}

type star struct {
	x, y, z float64
	speed   float64
	hue     float64
}

type Starfield struct {
	stars     []star
	rng       uint64
	inited    bool
	grid      []rune
	colors    []int32 // packed RGB (-1 = no star)
	gridW     int
	gridH     int
	audioData *audio.FrequencyData
	intensity float64 // audio intensity, computed in Advance()
	beat      BeatDetector
}

func NewStarfield() *Starfield {
	return &Starfield{}
}

func (sf *Starfield) Init(seed string, durationMs int) {
	h := fnv.New64a()
	h.Write([]byte(seed))
	sf.rng = h.Sum64()
	sf.stars = make([]star, baseStars, maxStars)
	for i := range sf.stars {
		sf.stars[i] = sf.newStar(true)
	}
	sf.beat.Reset()
	sf.inited = true
}

func (sf *Starfield) SetAudioData(data *audio.FrequencyData) {
	sf.audioData = data
}

func (sf *Starfield) Advance() {
	if !sf.inited {
		return
	}

	// Audio-reactive parameters.
	speedMul := 0.08 // slow drift when idle/paused
	sf.intensity = 0
	if sf.audioData != nil {
		bass := float64(sf.audioData.Bass)
		mid := float64(sf.audioData.Mid)
		peak := float64(sf.audioData.Peak)
		sf.intensity = bass*0.5 + mid*0.3 + peak*0.2

		sf.beat.Tick(bass, sf.audioData.ProgressMs)

		// Speed: continuous bass drive + beat pulse burst, scaled by tempo.
		bassDrive := bass * 3.0
		beatBurst := sf.beat.Pulse * 4.0
		speedMul = (0.3 + bassDrive + beatBurst) * sf.beat.TempoMul

		// Spawn extra stars based on overall intensity.
		targetCount := baseStars + int(sf.intensity*float64(maxStars-baseStars))
		if targetCount > maxStars {
			targetCount = maxStars
		}
		for len(sf.stars) < targetCount {
			sf.stars = append(sf.stars, sf.newStar(false))
		}
	} else {
		// No audio: gradually shed extra stars back to base count.
		if len(sf.stars) > baseStars {
			newLen := len(sf.stars) - 2
			if newLen < baseStars {
				newLen = baseStars
			}
			sf.stars = sf.stars[:newLen]
		}
	}

	dt := 0.015 * speedMul
	alive := sf.stars[:0]
	for i := range sf.stars {
		s := &sf.stars[i]
		s.z -= s.speed * dt
		if s.z <= 0.01 {
			// Recycle star if we're at or below target count, otherwise drop it.
			if sf.audioData == nil && len(alive) >= baseStars {
				continue
			}
			sf.stars[i] = sf.newStar(false)
		}
		alive = append(alive, sf.stars[i])
	}
	sf.stars = alive
}

func (sf *Starfield) View(width, height int) string {
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

		lum := 0.45 + closeness*0.4 + sf.intensity*0.15
		if lum > 1.0 {
			lum = 1.0
		}
		hue := s.hue
		sat := 0.4 + closeness*0.3 + sf.intensity*0.3
		if sat > 1.0 {
			sat = 1.0
		}

		r, g, b := hslToRGB(hue, sat, lum)
		idx := row*width + col
		sf.grid[idx] = starChars[charIdx]
		sf.colors[idx] = int32(r)<<16 | int32(g)<<8 | int32(b)
	}

	var buf strings.Builder
	buf.Grow(size * 20)

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

	// Extra scramble to decouple hue from x/y position.
	sf.rng = xorshift(xorshift(xorshift(sf.rng)))
	hue := themeHueStart + float64(sf.rng%1500)/10.0 // 130–280

	z := 1.0
	if randomDepth {
		sf.rng = xorshift(sf.rng)
		z = float64(sf.rng%999+1) / 1000.0
	}
	return star{x: x, y: y, z: z, speed: speed, hue: hue}
}
