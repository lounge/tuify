package audio

import (
	"math"
	"math/cmplx"

	"github.com/madelynnblue/go-dsp/fft"
)

// peakDecay controls how fast the running peak normalizer decays per FFT frame (~46 ms).
const peakDecay = 0.999

// Analyzer performs FFT analysis on PCM audio chunks and produces FrequencyData.
type Analyzer struct {
	window   []float64 // precomputed Hann window coefficients
	peakMax  float64   // running peak for spectral band normalization, with decay
	levelMax float64   // running peak for time-domain L/R level normalization, with decay
}

// NewAnalyzer creates an Analyzer with a precomputed Hann window of the given size.
func NewAnalyzer(windowSize int) *Analyzer {
	w := make([]float64, windowSize)
	for i := range w {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(windowSize-1)))
	}
	return &Analyzer{window: w, peakMax: 1.0, levelMax: 1.0}
}

// Analyze takes interleaved stereo int16 PCM samples and returns FrequencyData.
// The samples slice must contain at least WindowSize*2 values (stereo pairs).
func (a *Analyzer) Analyze(samples []int16) FrequencyData {
	n := len(a.window)
	mono := make([]float64, n)

	// Mix stereo to mono for the FFT, and track per-channel peak amplitude
	// in the time domain so visualizers like the VU meter can read true
	// stereo loudness instead of the post-mix spectral peak.
	var lPeak, rPeak int32
	for i := range n {
		si := i * 2
		if si+1 < len(samples) {
			l := int32(samples[si])
			r := int32(samples[si+1])
			mono[i] = (float64(l) + float64(r)) / 2.0
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
	}

	// Apply Hann window.
	for i := range n {
		mono[i] *= a.window[i]
	}

	// Run FFT.
	spectrum := fft.FFTReal(mono)

	// Map FFT bins to 64 logarithmically spaced frequency bands (20 Hz – 20 kHz).
	var fd FrequencyData
	nyquist := float64(DefaultFormat.SampleRate) / 2.0
	binHz := nyquist / float64(n/2)

	minFreq := 20.0
	maxFreq := 20000.0
	logMin := math.Log10(minFreq)
	logMax := math.Log10(maxFreq)

	for band := range NumBands {
		// Logarithmic band edges.
		loFreq := math.Pow(10, logMin+(logMax-logMin)*float64(band)/float64(NumBands))
		hiFreq := math.Pow(10, logMin+(logMax-logMin)*float64(band+1)/float64(NumBands))

		loBin := int(loFreq / binHz)
		hiBin := int(hiFreq / binHz)
		if loBin < 0 {
			loBin = 0
		}
		halfN := n / 2
		if hiBin >= halfN {
			hiBin = halfN - 1
		}
		if loBin > hiBin {
			loBin = hiBin
		}

		// Average magnitude across bins in this band.
		var sum float64
		count := 0
		for bi := loBin; bi <= hiBin; bi++ {
			mag := cmplx.Abs(spectrum[bi])
			sum += mag
			count++
		}
		if count > 0 {
			fd.Bands[band] = float32(sum / float64(count))
		}
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
	chanMax := lPeak
	if rPeak > chanMax {
		chanMax = rPeak
	}
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
