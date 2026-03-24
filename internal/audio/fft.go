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
	window  []float64 // precomputed Hann window coefficients
	peakMax float64   // running peak for normalization, with decay
}

// NewAnalyzer creates an Analyzer with a precomputed Hann window of the given size.
func NewAnalyzer(windowSize int) *Analyzer {
	w := make([]float64, windowSize)
	for i := range w {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(windowSize-1)))
	}
	return &Analyzer{window: w, peakMax: 1.0}
}

// Analyze takes interleaved stereo int16 PCM samples and returns FrequencyData.
// The samples slice must contain at least WindowSize*2 values (stereo pairs).
func (a *Analyzer) Analyze(samples []int16) FrequencyData {
	n := len(a.window)
	mono := make([]float64, n)

	// Mix stereo to mono by averaging left and right channels.
	for i := range n {
		si := i * 2
		if si+1 < len(samples) {
			mono[i] = (float64(samples[si]) + float64(samples[si+1])) / 2.0
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

	fd.ComputeConvenienceFields()

	return fd
}
