package visualizers

import (
	"math"
	"strings"

	"github.com/lounge/tuify/internal/audio"
)

// Spectrogram renders a scrolling time-frequency plot. Each Advance pushes
// one frame on the right edge; older frames scroll left. Vertical axis is
// frequency (low at the bottom, high at the top); color is magnitude using
// matplotlib's "inferno" palette (near-black → deep purple → magenta →
// orange → yellow). This is the standard colormap for audio spectrograms.
//
// Quadrant rendering packs four sub-cells into each character (2 time steps
// × 2 frequency bins), so an H×W grid encodes 2H × 2W data points. A
// terminal character can only hold one fg + one bg, so we rank the four
// sub-cell amplitudes, assign the top two to the fg and the bottom two to
// the bg, then pick the unicode quadrant glyph whose filled pixels match
// the hot pair. Uniform cells collapse to a solid block to avoid stair-
// step artifacts when all four amps are essentially equal.
//
// To prevent ranker flicker on noisy bright regions, the stored frames are
// mildly smoothed with an EMA. Noise is the root cause of the flicker —
// smoothing the source is more robust than scaling thresholds at render
// time, and it doesn't create boundary transitions between rendering modes.

const (
	// spectroMaxWidth sizes the ring buffer. Far wider than any realistic
	// terminal so the history never truncates when the user resizes down.
	// Memory cost: 512 * 64 * 4 bytes = 128 KB per spectrogram instance.
	spectroMaxWidth = 512

	// spectroDecay fades old frames when there's no audio so the image
	// keeps scrolling instead of freezing. A frozen spectrogram reads as
	// "broken"; a fading one reads as "paused."
	spectroDecay = float32(0.90)

	// spectroGamma < 1 brightens mid-range signals before the colormap
	// lookup. Audio energy is concentrated in the 0.1–0.5 range; without
	// gamma, most of the palette's interesting colors go unused.
	spectroGamma = 0.60

	// spectroSmooth is the EMA coefficient applied when storing new
	// frames: new_stored = prev_stored * spectroSmooth + fresh * (1 - spectroSmooth).
	// 0.55 is a light touch — transients remain crisp but frame-to-frame
	// noise in bright sustained passages drops enough that the 4-way
	// ranker doesn't flip glyphs on meaningless jitter. Tune higher for
	// more smoothing, lower for more twitchy response.
	spectroSmooth = float32(0.55)

	// spectroFlatSpread: when all four sub-cells of a character are within
	// this range of each other, render a solid block with the mean color
	// instead of picking a quadrant. Otherwise the ranker's tie-breaking
	// creates noisy flicker in uniform regions.
	spectroFlatSpread = float32(0.04)
)

type Spectrogram struct {
	audioData *audio.FrequencyData

	// Ring buffer of frequency frames. Writes land at head, which then
	// advances. View reads backward from head-1 to fill newest-first.
	frames [spectroMaxWidth][audio.NumBands]float32
	head   int
	inited bool
}

func NewSpectrogram() *Spectrogram { return &Spectrogram{} }

func (s *Spectrogram) Init(seed string, durationMs int) {
	s.frames = [spectroMaxWidth][audio.NumBands]float32{}
	s.head = 0
	s.inited = true
}

func (s *Spectrogram) SetAudioData(data *audio.FrequencyData) {
	s.audioData = data
}

// Advance appends the current frame to the ring buffer. With live audio we
// mix the new bands into the previous frame via EMA so frame-to-frame noise
// doesn't drive the quadrant ranker into visible flicker. Without audio we
// push a decayed copy so the image keeps scrolling with a visible fade
// instead of freezing.
func (s *Spectrogram) Advance() {
	if !s.inited {
		return
	}
	var frame [audio.NumBands]float32
	prev := (s.head - 1 + spectroMaxWidth) % spectroMaxWidth
	if s.audioData != nil {
		for i, v := range s.audioData.Bands {
			frame[i] = s.frames[prev][i]*spectroSmooth + v*(1-spectroSmooth)
		}
	} else {
		for i, v := range s.frames[prev] {
			frame[i] = v * spectroDecay
		}
	}
	s.frames[s.head] = frame
	s.head = (s.head + 1) % spectroMaxWidth
}

func (s *Spectrogram) View(width, height int) string {
	if !s.inited || width < 1 || height < 1 {
		return ""
	}
	// Each character encodes 2 time columns — clamp so the history we
	// read from the ring buffer never wraps past itself.
	if 2*width > spectroMaxWidth {
		width = spectroMaxWidth / 2
	}

	var buf strings.Builder
	// ANSI fg+bg escape + quadrant glyph is ~30 bytes per cell.
	buf.Grow(width * height * 40)

	totalFreqSlots := 2 * height

	for row := range height {
		// Freq slots, inverted so low freq sits at the bottom of the screen.
		// freqUp is the upper half of the character (higher frequency);
		// freqLow is the lower half.
		freqUp := 2*(height-1-row) + 1
		freqLow := 2 * (height - 1 - row)

		for col := range width {
			// Right time step of the char is newer; left is older.
			rightAge := (width - 1 - col) * 2
			leftAge := rightAge + 1
			rightIdx := (s.head - 1 - rightAge + spectroMaxWidth) % spectroMaxWidth
			leftIdx := (s.head - 1 - leftAge + spectroMaxWidth) % spectroMaxWidth

			tl := s.bandAvg(leftIdx, freqUp, totalFreqSlots)
			tr := s.bandAvg(rightIdx, freqUp, totalFreqSlots)
			bl := s.bandAvg(leftIdx, freqLow, totalFreqSlots)
			br := s.bandAvg(rightIdx, freqLow, totalFreqSlots)

			glyph, hot, cold := pickQuadrant(tl, tr, bl, br)

			fgR, fgG, fgB := infernoColor(hot)
			bgR, bgG, bgB := infernoColor(cold)
			writeAnsiFgBg(&buf, fgR, fgG, fgB, bgR, bgG, bgB)
			buf.WriteString(glyph)
		}
		// Reset before the newline so the trailing bg color doesn't bleed
		// onto unrendered columns or the next row.
		buf.WriteString(ansiReset)
		if row < height-1 {
			buf.WriteRune('\n')
		}
	}
	return buf.String()
}

// bandAvg averages the band range mapped to a freq slot. Slot 0 = lowest
// freq; slot totalFreqSlots-1 = highest.
func (s *Spectrogram) bandAvg(frameIdx, freqSlot, totalFreqSlots int) float32 {
	bandLo := freqSlot * audio.NumBands / totalFreqSlots
	bandHi := (freqSlot + 1) * audio.NumBands / totalFreqSlots
	if bandHi <= bandLo {
		bandHi = bandLo + 1
	}
	if bandHi > audio.NumBands {
		bandHi = audio.NumBands
	}
	var sum float32
	for b := bandLo; b < bandHi; b++ {
		sum += s.frames[frameIdx][b]
	}
	return sum / float32(bandHi-bandLo)
}

// pickQuadrant ranks the four sub-cell amplitudes, assigns the top two to
// the foreground (hot) and the bottom two to the background (cold), and
// returns the quadrant glyph plus the mean hot/cold amplitudes. Uniform
// regions collapse to a solid block to avoid flicker from rank ties.
func pickQuadrant(tl, tr, bl, br float32) (string, float32, float32) {
	// Unrolled min/max over tr/bl/br is faster and cheaper than ranging
	// over a literal slice (no backing array allocation).
	lo, hi := tl, tl
	if tr < lo {
		lo = tr
	} else if tr > hi {
		hi = tr
	}
	if bl < lo {
		lo = bl
	} else if bl > hi {
		hi = bl
	}
	if br < lo {
		lo = br
	} else if br > hi {
		hi = br
	}
	if hi-lo < spectroFlatSpread {
		mean := (tl + tr + bl + br) / 4
		return "█", mean, mean
	}

	vals := [4]float32{tl, tr, bl, br}
	idx := [4]int{0, 1, 2, 3}
	// Sort indices descending by amplitude. 4 elements — bubble sort is fine.
	for i := range idx {
		for j := i + 1; j < 4; j++ {
			if vals[idx[i]] < vals[idx[j]] {
				idx[i], idx[j] = idx[j], idx[i]
			}
		}
	}
	pattern := quadrantBits[idx[0]] | quadrantBits[idx[1]]
	hot := (vals[idx[0]] + vals[idx[1]]) / 2
	cold := (vals[idx[2]] + vals[idx[3]]) / 2
	return quadrantGlyphs[pattern], hot, cold
}

// infernoStops are hand-picked samples of matplotlib's "inferno" colormap.
// Eight stops is enough for a smooth perceptual gradient once expanded via
// buildPaletteLUT256 into a 256-entry lookup table.
var infernoStops = []paletteStop{
	{0.00, 0, 0, 4},
	{0.13, 22, 11, 57},
	{0.26, 66, 10, 104},
	{0.39, 120, 28, 109},
	{0.52, 169, 51, 91},
	{0.65, 215, 79, 56},
	{0.78, 245, 128, 32},
	{0.91, 251, 190, 67},
	{1.00, 252, 254, 164},
}

// infernoLUT is a precomputed 256-entry lookup baked from infernoStops.
// Cell rendering happens tens of thousands of times per frame, so avoiding
// piecewise-linear math per pixel is worth the 768 bytes of static table.
var infernoLUT = buildPaletteLUT256(infernoStops)

// infernoColor maps a normalized amplitude (0–1) to an inferno-palette RGB.
// Applies spectroGamma first so most of the palette's range lands on the
// mid-amplitudes where real audio lives.
func infernoColor(amp float32) (int, int, int) {
	a := math.Pow(clampF64(float64(amp), 0, 1), spectroGamma)
	c := infernoLUT[int(a*255)]
	return int(c[0]), int(c[1]), int(c[2])
}
