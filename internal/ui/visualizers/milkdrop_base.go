package visualizers

import (
	"math"
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

// pixel holds a single framebuffer cell in HSL color space.
type pixel struct{ h, s, l float64 }

// warpFunc transforms a normalized coordinate (nx, ny in [-1,+1]) into a source
// coordinate to sample from the previous frame. t is elapsed time in seconds;
// bass and mid are smoothed audio values in [0,1].
type warpFunc func(nx, ny, t, bass, mid float64) (float64, float64)

// milkdropBase provides the shared framebuffer, feedback warp loop, audio
// smoothing, and half-block rendering used by all Milkdrop-style presets.
type milkdropBase struct {
	fb     []pixel // current framebuffer, len = fbW * fbH
	fbBack []pixel // previous frame (swap buffer)
	fbW    int     // pixel columns = terminal width
	fbH    int     // pixel rows = terminal height * 2 (half-block doubling)
	tick   float64 // monotonic time in seconds

	// Smoothed audio values (EMA).
	bass   float64
	mid    float64
	high   float64
	energy float64

	audioData *audio.FrequencyData
	inited    bool
}

const (
	mdDt       = 0.033 // seconds per tick (~30 fps)
	mdEMA      = 0.3   // EMA alpha for audio smoothing
	mdDecayL   = 0.97  // luminance decay per frame
	mdDecayS   = 0.999 // saturation decay per frame (keep colors vivid)
	mdIdleDecay = 0.95 // audio value decay when no audio
	mdHueSpeed = 90.0  // degrees/sec of color cycling at full energy
)

func (m *milkdropBase) mdInit() {
	m.fb = make([]pixel, 4)
	m.fbBack = make([]pixel, 4)
	m.fbW = 2
	m.fbH = 2
	m.tick = 0
	m.bass = 0
	m.mid = 0
	m.high = 0
	m.energy = 0
	m.inited = true
}

func (m *milkdropBase) mdSetAudioData(data *audio.FrequencyData) {
	m.audioData = data
}

func (m *milkdropBase) resize(termW, termH int) {
	pixH := termH * 2
	if m.fbW == termW && m.fbH == pixH {
		return
	}
	m.fbW = termW
	m.fbH = pixH
	size := termW * pixH
	m.fb = make([]pixel, size)
	m.fbBack = make([]pixel, size)
}

func (m *milkdropBase) updateAudio() {
	if m.audioData != nil {
		m.bass += mdEMA * (float64(m.audioData.Bass) - m.bass)
		m.mid += mdEMA * (float64(m.audioData.Mid) - m.mid)
		m.high += mdEMA * (float64(m.audioData.High) - m.high)
	} else {
		m.bass *= mdIdleDecay
		m.mid *= mdIdleDecay
		m.high *= mdIdleDecay
	}
	m.energy = m.bass*0.5 + m.mid*0.3 + m.high*0.2
}

// sampleBilinear reads a pixel from fbBack at normalized coords (sx, sy) in
// [-1,+1] using bilinear interpolation with toroidal wrapping.
func (m *milkdropBase) sampleBilinear(sx, sy float64) pixel {
	// Convert normalized [-1,+1] to pixel coords.
	px := (sx + 1) * 0.5 * float64(m.fbW)
	py := (sy + 1) * 0.5 * float64(m.fbH)

	fw := float64(m.fbW)
	fh := float64(m.fbH)

	// Toroidal wrap.
	px = math.Mod(px, fw)
	if px < 0 {
		px += fw
	}
	py = math.Mod(py, fh)
	if py < 0 {
		py += fh
	}

	x0 := int(px)
	y0 := int(py)
	fx := px - float64(x0)
	fy := py - float64(y0)
	x1 := (x0 + 1) % m.fbW
	y1 := (y0 + 1) % m.fbH

	p00 := m.fbBack[y0*m.fbW+x0]
	p10 := m.fbBack[y0*m.fbW+x1]
	p01 := m.fbBack[y1*m.fbW+x0]
	p11 := m.fbBack[y1*m.fbW+x1]

	return pixel{
		h: lerpAngle(p00.h, p10.h, fx)*(1-fy) + lerpAngle(p01.h, p11.h, fx)*fy,
		s: (p00.s*(1-fx)+p10.s*fx)*(1-fy) + (p01.s*(1-fx)+p11.s*fx)*fy,
		l: (p00.l*(1-fx)+p10.l*fx)*(1-fy) + (p01.l*(1-fx)+p11.l*fx)*fy,
	}
}

// lerpAngle interpolates two hue values (0–360) taking the shortest arc.
func lerpAngle(a, b, t float64) float64 {
	diff := b - a
	if diff > 180 {
		diff -= 360
	} else if diff < -180 {
		diff += 360
	}
	h := a + diff*t
	if h < 0 {
		h += 360
	} else if h >= 360 {
		h -= 360
	}
	return h
}

func (m *milkdropBase) advanceBase(warp warpFunc) {
	if !m.inited {
		return
	}
	m.updateAudio()
	m.tick += mdDt

	// Swap buffers: current becomes previous.
	m.fb, m.fbBack = m.fbBack, m.fb

	// Warp feedback loop.
	for py := range m.fbH {
		ny := float64(py)/float64(m.fbH)*2 - 1 // normalized y [-1,+1]
		for px := range m.fbW {
			nx := float64(px)/float64(m.fbW)*2 - 1 // normalized x [-1,+1]

			sx, sy := warp(nx, ny, m.tick, m.bass, m.mid)
			p := m.sampleBilinear(sx, sy)

			// Decay.
			p.l *= mdDecayL
			p.s *= mdDecayS

			// Audio pumps saturation back up — keeps trails vivid.
			if m.energy > 0.05 {
				p.s += (1 - p.s) * m.energy * 0.15
			}

			// Color cycling.
			p.h += mdHueSpeed * m.energy * mdDt
			p.h = math.Mod(p.h, 360)
			if p.h < 0 {
				p.h += 360
			}

			m.fb[py*m.fbW+px] = p
		}
	}

	// Stamp energy: expanding ring from center.
	m.stampEnergy()
}

func (m *milkdropBase) stampEnergy() {
	if m.energy < 0.01 {
		return
	}
	cx := float64(m.fbW) / 2
	cy := float64(m.fbH) / 2
	maxR := math.Sqrt(cx*cx + cy*cy)

	for py := range m.fbH {
		dy := float64(py) - cy
		for px := range m.fbW {
			dx := float64(px) - cx
			r := math.Sqrt(dx*dx+dy*dy) / maxR // 0–1

			// Thin expanding ring.
			wave := math.Sin(r*15 - m.tick*4)
			brightness := math.Max(0, 1-3*math.Abs(wave)) * m.energy * 0.5

			if brightness > 0.001 {
				idx := py*m.fbW + px
				p := &m.fb[idx]
				p.l += brightness
				if p.l > 1 {
					p.l = 1
				}
				// Stamp vivid color on fresh energy.
				p.s = 0.85 + m.energy*0.15
				p.h = math.Mod(m.tick*40+r*180, 360)
			}
		}
	}
}

func (m *milkdropBase) render(termW, termH int) string {
	var buf strings.Builder
	buf.Grow(termW * termH * 24)

	for row := range termH {
		topRow := row * 2
		botRow := topRow + 1
		for col := range termW {
			top := m.fb[topRow*m.fbW+col]
			bot := m.fb[botRow*m.fbW+col]

			tr, tg, tb := hslToRGB(top.h, top.s, top.l)
			br, bg, bb := hslToRGB(bot.h, bot.s, bot.l)

			buf.WriteString(ansiFgBg(tr, tg, tb, br, bg, bb))
			buf.WriteString("▀")
		}
		buf.WriteString(ansiReset)
		if row < termH-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}
