package audio

import (
	"math"
	"math/bits"
)

// peakDecay controls how fast the running peak normalizer decays per FFT frame (~46 ms).
const peakDecay = 0.999

// analyzer performs FFT analysis on PCM audio chunks and produces FrequencyData.
// It owns its FFT work buffers, so Analyze does not allocate; an analyzer
// must not be used from more than one goroutine at a time.
type analyzer struct {
	window   []float64 // precomputed Hann window coefficients
	re, im   []float64 // FFT work buffers, reused across frames
	cos, sin []float64 // twiddle factors e^(-2πik/n) for k < n/2
	bitrev   []int     // input index → bit-reversed FFT slot
	loBin    [NumBands]int
	hiBin    [NumBands]int // inclusive FFT bin range averaged into each band
	peakMax  float64       // running peak for spectral band normalization, with decay
	levelMax float64       // running peak for time-domain L/R level normalization, with decay
}

// newAnalyzer creates an analyzer with a precomputed Hann window, FFT
// tables and band-to-bin map for the given window size. windowSize must be
// a power of two of at least 2; newAnalyzer panics otherwise.
func newAnalyzer(windowSize int) *analyzer {
	n := windowSize
	if n < 2 || n&(n-1) != 0 {
		panic("audio.newAnalyzer: windowSize must be a power of two >= 2")
	}
	a := &analyzer{
		window:   make([]float64, n),
		re:       make([]float64, n),
		im:       make([]float64, n),
		cos:      make([]float64, n/2),
		sin:      make([]float64, n/2),
		bitrev:   make([]int, n),
		peakMax:  1.0,
		levelMax: 1.0,
	}
	for i := range n {
		a.window[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	for k := range n / 2 {
		angle := -2 * math.Pi * float64(k) / float64(n)
		a.cos[k], a.sin[k] = math.Cos(angle), math.Sin(angle)
	}
	shift := 64 - bits.TrailingZeros(uint(n))
	for i := range n {
		a.bitrev[i] = int(bits.Reverse64(uint64(i)) >> shift)
	}

	// Map FFT bins to 64 logarithmically spaced frequency bands (20 Hz – 20 kHz).
	nyquist := float64(defaultFormat.SampleRate) / 2.0
	binHz := nyquist / float64(n/2)
	logMin := math.Log10(20.0)
	logMax := math.Log10(20000.0)
	for band := range NumBands {
		// Logarithmic band edges.
		loFreq := math.Pow(10, logMin+(logMax-logMin)*float64(band)/float64(NumBands))
		hiFreq := math.Pow(10, logMin+(logMax-logMin)*float64(band+1)/float64(NumBands))

		loBin := max(int(loFreq/binHz), 0)
		hiBin := min(int(hiFreq/binHz), n/2-1)
		a.loBin[band] = min(loBin, hiBin)
		a.hiBin[band] = hiBin
	}
	return a
}

// Analyze takes interleaved stereo int16 PCM samples and returns FrequencyData.
// The samples slice must contain at least WindowSize*2 values (stereo pairs).
func (a *analyzer) Analyze(samples []int16) FrequencyData {
	n := len(a.window)
	re, im := a.re, a.im
	clear(im)

	// Mix stereo to mono, apply the Hann window, and store each sample at
	// its bit-reversed slot so the FFT below can run in place. Track
	// per-channel peak amplitude in the time domain so visualizers like the
	// VU meter can read true stereo loudness instead of the post-mix
	// spectral peak.
	var lPeak, rPeak int32
	for i := range n {
		si := i * 2
		var mono float64
		if si+1 < len(samples) {
			l := int32(samples[si])
			r := int32(samples[si+1])
			mono = (float64(l) + float64(r)) / 2.0
			if l < 0 {
				l = -l
			}
			if r < 0 {
				r = -r
			}
			if l > lPeak {
				lPeak = l
			}
			if r > rPeak {
				rPeak = r
			}
		}
		re[a.bitrev[i]] = mono * a.window[i]
	}

	a.fft()

	// Average magnitude across the bins in each band.
	var fd FrequencyData
	for band := range NumBands {
		lo, hi := a.loBin[band], a.hiBin[band]
		var sum float64
		for bi := lo; bi <= hi; bi++ {
			sum += math.Sqrt(re[bi]*re[bi] + im[bi]*im[bi])
		}
		fd.Bands[band] = float32(sum / float64(hi-lo+1))
	}

	// Find peak across all bands for normalization.
	var maxBand float32
	for _, b := range fd.Bands {
		if b > maxBand {
			maxBand = b
		}
	}

	// Update running peak with slow decay for stable normalization.
	if float64(maxBand) > a.peakMax {
		a.peakMax = float64(maxBand)
	} else {
		a.peakMax *= peakDecay
	}
	if a.peakMax < 1.0 {
		a.peakMax = 1.0
	}

	// Normalize bands to 0.0–1.0.
	scale := float32(1.0 / a.peakMax)
	for i := range fd.Bands {
		fd.Bands[i] *= scale
		if fd.Bands[i] > 1.0 {
			fd.Bands[i] = 1.0
		}
	}

	// Peak from normalized bands — represents instantaneous loudness (0–1).
	fd.Peak = 0
	for _, b := range fd.Bands {
		if b > fd.Peak {
			fd.Peak = b
		}
	}

	// Per-channel time-domain levels with a shared running-max AGC so
	// stereo balance is preserved across L and R while overall track
	// loudness is adapted to.
	chanMax := max(rPeak, lPeak)
	if float64(chanMax) > a.levelMax {
		a.levelMax = float64(chanMax)
	} else {
		a.levelMax *= peakDecay
	}
	if a.levelMax < 1.0 {
		a.levelMax = 1.0
	}
	levelScale := 1.0 / a.levelMax
	fd.LeftLevel = float32(math.Min(float64(lPeak)*levelScale, 1.0))
	fd.RightLevel = float32(math.Min(float64(rPeak)*levelScale, 1.0))

	fd.ComputeConvenienceFields()

	return fd
}

// fft runs an in-place iterative radix-2 FFT over re/im, which must
// already hold the input in bit-reversed order.
func (a *analyzer) fft() {
	re, im := a.re, a.im
	n := len(re)
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		for start := 0; start < n; start += size {
			for k := range half {
				wr, wi := a.cos[k*step], a.sin[k*step]
				j, l := start+k, start+k+half
				tr := wr*re[l] - wi*im[l]
				ti := wr*im[l] + wi*re[l]
				re[l], im[l] = re[j]-tr, im[j]-ti
				re[j] += tr
				im[j] += ti
			}
		}
	}
}
