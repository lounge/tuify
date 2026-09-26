package audio

import (
	"math"
	"testing"
)

func TestAnalyzeSilence(t *testing.T) {
	t.Parallel()

	a := NewAnalyzer(WindowSize)
	samples := make([]int16, WindowSize*2) // stereo silence
	fd := a.Analyze(samples)

	for i, b := range fd.Bands {
		if b != 0 {
			t.Errorf("band %d = %f, want 0", i, b)
		}
	}
	if fd.Bass != 0 || fd.Mid != 0 || fd.High != 0 || fd.Peak != 0 {
		t.Errorf("expected all zeros for silence, got bass=%f mid=%f high=%f peak=%f",
			fd.Bass, fd.Mid, fd.High, fd.Peak)
	}
}

func TestAnalyzeSineWave(t *testing.T) {
	t.Parallel()

	a := NewAnalyzer(WindowSize)

	// Generate a 1 kHz sine wave at full scale, stereo.
	freq := 1000.0
	samples := make([]int16, WindowSize*2)
	for i := range WindowSize {
		val := int16(32000 * math.Sin(2*math.Pi*freq*float64(i)/float64(DefaultFormat.SampleRate)))
		samples[i*2] = val   // left
		samples[i*2+1] = val // right
	}

	fd := a.Analyze(samples)

	// 1 kHz should land in the mid-frequency range (bands ~18-25 for log spacing 20Hz-20kHz).
	// Find the peak band.
	peakBand := 0
	var peakVal float32
	for i, b := range fd.Bands {
		if b > peakVal {
			peakVal = b
			peakBand = i
		}
	}

	// 1 kHz with 64 log bands from 20Hz-20kHz: log10(1000) = 3.0, range is log10(20)=1.3 to log10(20000)=4.3
	// band = (3.0 - 1.3) / (4.3 - 1.3) * 64 ≈ 36. Allow a range.
	if peakBand < 30 || peakBand > 42 {
		t.Errorf("1 kHz sine peak at band %d (val %f), expected roughly 30-42", peakBand, peakVal)
	}

	if peakVal < 0.5 {
		t.Errorf("peak value %f too low for full-scale sine", peakVal)
	}

	// Bass should be near zero for a 1 kHz signal.
	if fd.Bass > 0.1 {
		t.Errorf("bass = %f, expected near zero for 1 kHz sine", fd.Bass)
	}
}

func TestAnalyzeDeterministic(t *testing.T) {
	t.Parallel()

	a := NewAnalyzer(WindowSize)

	samples := make([]int16, WindowSize*2)
	for i := range samples {
		samples[i] = int16(i % 1000)
	}

	fd1 := a.Analyze(samples)

	// Reset analyzer to same initial state.
	a2 := NewAnalyzer(WindowSize)
	fd2 := a2.Analyze(samples)

	for i := range fd1.Bands {
		if fd1.Bands[i] != fd2.Bands[i] {
			t.Errorf("band %d: %f != %f", i, fd1.Bands[i], fd2.Bands[i])
		}
	}
}

func TestAnalyzeLowFrequency(t *testing.T) {
	t.Parallel()

	a := NewAnalyzer(WindowSize)

	// Generate a 60 Hz sine wave (bass range).
	freq := 60.0
	samples := make([]int16, WindowSize*2)
	for i := range WindowSize {
		val := int16(32000 * math.Sin(2*math.Pi*freq*float64(i)/float64(DefaultFormat.SampleRate)))
		samples[i*2] = val
		samples[i*2+1] = val
	}

	fd := a.Analyze(samples)

	// 60 Hz should be in the bass region (first few bands).
	if fd.Bass < 0.05 {
		t.Errorf("bass = %f, expected significant energy for 60 Hz sine", fd.Bass)
	}

	// High frequencies should be near zero.
	if fd.High > 0.05 {
		t.Errorf("high = %f, expected near zero for 60 Hz sine", fd.High)
	}
}

// TestFFTMatchesDFT checks the in-place radix-2 FFT against a direct O(n²)
// DFT of the same input, bin by bin.
func TestFFTMatchesDFT(t *testing.T) {
	t.Parallel()

	const n = 256
	a := NewAnalyzer(n)
	in := make([]float64, n)
	x := uint32(1)
	for i := range in {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		in[i] = float64(int16(x))
	}
	clear(a.im)
	for i, v := range in {
		a.re[a.bitrev[i]] = v
	}
	a.fft()

	for k := range n {
		var wantRe, wantIm float64
		for i, v := range in {
			angle := -2 * math.Pi * float64(k*i) / n
			wantRe += v * math.Cos(angle)
			wantIm += v * math.Sin(angle)
		}
		if math.Abs(a.re[k]-wantRe) > 1e-6*n*32768 || math.Abs(a.im[k]-wantIm) > 1e-6*n*32768 {
			t.Fatalf("bin %d = (%g, %g), want (%g, %g)", k, a.re[k], a.im[k], wantRe, wantIm)
		}
	}
}

func TestNewAnalyzer_PanicsOnNonPowerOfTwo(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 1, 3, 1000} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewAnalyzer(%d) should panic", n)
				}
			}()
			NewAnalyzer(n)
		}()
	}
}
