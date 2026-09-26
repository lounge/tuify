package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"strconv"
	"testing"
)

// benchSamples returns one FFT window of interleaved stereo PCM holding a
// 440 Hz + 3 kHz mix, so both low and mid bands carry energy.
func benchSamples() []int16 {
	s := make([]int16, WindowSize*2)
	rate := float64(DefaultFormat.SampleRate)
	for i := range WindowSize {
		t := float64(i) / rate
		v := int16(8000*math.Sin(2*math.Pi*440*t) + 8000*math.Sin(2*math.Pi*3000*t))
		s[i*2], s[i*2+1] = v, v
	}
	return s
}

func BenchmarkAnalyze(b *testing.B) {
	a := NewAnalyzer(WindowSize)
	samples := benchSamples()
	b.ReportAllocs()
	for b.Loop() {
		a.Analyze(samples)
	}
}

// cyclicReader serves buf endlessly, like a pipe that never runs dry.
type cyclicReader struct {
	buf []byte
	off int
}

func (r *cyclicReader) Read(p []byte) (int, error) {
	n := copy(p, r.buf[r.off:])
	r.off = (r.off + n) % len(r.buf)
	return n, nil
}

func BenchmarkBridgeRead(b *testing.B) {
	var raw bytes.Buffer
	for _, v := range benchSamples() {
		_ = binary.Write(&raw, binary.LittleEndian, v)
	}
	for _, size := range []int{4096, 8192} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			br := &pipeReaderBridge{
				pipe:     &cyclicReader{buf: raw.Bytes()},
				analyzer: NewAnalyzer(WindowSize),
				format:   DefaultFormat,
				store:    func(*FrequencyData) {},
			}
			p := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := br.Read(p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
