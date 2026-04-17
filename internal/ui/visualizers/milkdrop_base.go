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

const (
	mdDt        = 0.033 // seconds per tick (~30 fps)
	mdEMA       = 0.3   // EMA alpha for audio smoothing
	mdDecayL    = 0.97  // luminance decay per frame
	mdDecayS    = 0.995 // saturation decay per frame
	mdIdleDecay = 0.95  // audio value decay when no audio
	mdHueSpeed  = 60.0  // degrees/sec of color cycling at full energy

	// Energy mix weights for computing overall energy from frequency bands.
	mdEnergyBass = 0.5
	mdEnergyMid  = 0.3
	mdEnergyHigh = 0.2

	// stampEnergy parameters.
	mdRingFreq       = 15.0  // radial frequency of the energy ring wave
	mdRingSpeed      = 4.0   // temporal speed of the energy ring
	mdRingSharpness  = 3.0   // controls ring width (higher = thinner)
	mdRingBright     = 0.3   // energy ring brightness scale
	mdStampSatFloor  = 0.5   // minimum saturation for energized pixels
	mdStampSatScale  = 0.4   // saturation gain per unit energy
	mdStampHueDrift  = 30.0  // hue drift rate from time
	mdStampHueSpread = 120.0 // hue variation across radius
)

// MilkdropPreset is a Milkdrop-style visualizer driven by a pluggable warp
// function. Use the NewMilkdrop* constructors to create specific presets.
type MilkdropPreset struct {
	warp   warpFunc
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

func (m *MilkdropPreset) Init(seed string, durationMs int) {
	m.tick = 0
	m.bass = 0
	m.mid = 0
	m.high = 0
	m.energy = 0
	m.fbW = 0
	m.fbH = 0
	m.fb = nil
	m.fbBack = nil
	m.inited = true
}

func (m *MilkdropPreset) SetAudioData(data *audio.FrequencyData) {
	m.audioData = data
}

func (m *MilkdropPreset) Advance() {
	if !m.inited || m.fbW == 0 || m.fbH == 0 {
		return
	}
	m.updateAudio()
	m.tick += mdDt

	// Swap buffers: current becomes previous.
	m.fb, m.fbBack = m.fbBack, m.fb

	// Warp feedback loop.
	for py := range m.fbH {
		ny := float64(py)/float64(m.fbH)*2 - 1
		for px := range m.fbW {
			nx := float64(px)/float64(m.fbW)*2 - 1

			sx, sy := m.warp(nx, ny, m.tick, m.bass, m.mid)
			p := m.sampleBilinear(sx, sy)

			p.l *= mdDecayL
			p.s *= mdDecayS

			p.h += mdHueSpeed * m.energy * mdDt
			p.h = math.Mod(p.h, 360)
			if p.h < 0 {
				p.h += 360
			}

			m.fb[py*m.fbW+px] = p
		}
	}

	m.stampEnergy()
}

func (m *MilkdropPreset) View(w, h int) string {
	if !m.inited || w < 1 || h < 1 {
		return ""
	}
	m.resize(w, h)
	return m.render(w, h)
}

func (m *MilkdropPreset) resize(termW, termH int) {
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

func (m *MilkdropPreset) updateAudio() {
	if m.audioData != nil {
		m.bass += mdEMA * (float64(m.audioData.Bass) - m.bass)
		m.mid += mdEMA * (float64(m.audioData.Mid) - m.mid)
		m.high += mdEMA * (float64(m.audioData.High) - m.high)
	} else {
		m.bass *= mdIdleDecay
		m.mid *= mdIdleDecay
		m.high *= mdIdleDecay
	}
	m.energy = m.bass*mdEnergyBass + m.mid*mdEnergyMid + m.high*mdEnergyHigh
}

// sampleBilinear reads a pixel from fbBack at normalized coords (sx, sy) in
// [-1,+1] using bilinear interpolation with toroidal wrapping.
func (m *MilkdropPreset) sampleBilinear(sx, sy float64) pixel {
	px := (sx + 1) * 0.5 * float64(m.fbW)
	py := (sy + 1) * 0.5 * float64(m.fbH)

	fw := float64(m.fbW)
	fh := float64(m.fbH)

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

func (m *MilkdropPreset) stampEnergy() {
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
			r := math.Sqrt(dx*dx+dy*dy) / maxR

			wave := math.Sin(r*mdRingFreq - m.tick*mdRingSpeed)
			brightness := math.Max(0, 1-mdRingSharpness*math.Abs(wave)) * m.energy * mdRingBright

			if brightness > 0.001 {
				idx := py*m.fbW + px
				p := &m.fb[idx]
				p.l += brightness
				if p.l > 1 {
					p.l = 1
				}
				if p.s < mdStampSatFloor {
					p.s = mdStampSatFloor + m.energy*mdStampSatScale
					p.h = math.Mod(m.tick*mdStampHueDrift+r*mdStampHueSpread, 360)
				}
			}
		}
	}
}

func (m *MilkdropPreset) render(termW, termH int) string {
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

			writeAnsiFgBg(&buf, tr, tg, tb, br, bg, bb)
			buf.WriteString("▀")
		}
		buf.WriteString(ansiReset)
		if row < termH-1 {
			buf.WriteRune('\n')
		}
	}

	return buf.String()
}

// framebuffer returns the current pixel buffer for testing.
func (m *MilkdropPreset) framebuffer() []pixel {
	return m.fb
}
