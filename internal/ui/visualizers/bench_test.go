package visualizers

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/audio"
)

const benchW, benchH = 120, 40

// BenchmarkWriteAnsiFg measures the escape writer alone: the builder is
// only reset once it is large, so its buffer growth amortizes to ~0 and
// the reported allocs are the writer's own.
func BenchmarkWriteAnsiFg(b *testing.B) {
	const bufSize = 1 << 20
	var sb strings.Builder
	sb.Grow(bufSize)
	b.ReportAllocs()
	for b.Loop() {
		if sb.Len() > bufSize-32 {
			sb.Reset()
			sb.Grow(bufSize)
		}
		writeAnsiFg(&sb, 123, 145, 167)
	}
}

// benchFrame is a mid-loudness frame with a sloped spectrum so every band
// and meter has something to draw.
func benchFrame() *audio.FrequencyData {
	fd := &audio.FrequencyData{Peak: 0.7, LeftLevel: 0.7, RightLevel: 0.6}
	for i := range fd.Bands {
		fd.Bands[i] = 1 - float32(i)/audio.NumBands
	}
	fd.ComputeConvenienceFields()
	return fd
}

func benchImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 128, 255})
		}
	}
	return img
}

// BenchmarkAdvanceView measures one rendered frame (Advance + View) per
// visualizer at 120x40, fed with every input it opts into.
func BenchmarkAdvanceView(b *testing.B) {
	cases := []struct {
		name string
		new  func() Visualizer
	}{
		{"AlbumArt", func() Visualizer { return NewAlbumArt() }},
		{"Lyrics", func() Visualizer { return NewLyrics() }},
		{"MilkdropKaleidoscope", func() Visualizer { return NewMilkdropKaleidoscope() }},
		{"MilkdropRipple", func() Visualizer { return NewMilkdropRipple() }},
		{"MilkdropSpiral", func() Visualizer { return NewMilkdropSpiral() }},
		{"MilkdropTunnel", func() Visualizer { return NewMilkdropTunnel() }},
		{"Oscillogram", func() Visualizer { return NewOscillogram() }},
		{"Spectrogram", func() Visualizer { return NewSpectrogram() }},
		{"Spectrum", func() Visualizer { return NewSpectrum() }},
		{"Starfield", func() Visualizer { return NewStarfield() }},
		{"VUMeter", func() Visualizer { return NewVUMeter() }},
	}
	frame, img := benchFrame(), benchImage()
	lyrics := strings.Split(strings.Repeat("a line of lyrics to scroll past\n", 40), "\n")
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			v := tc.new()
			v.Init("bench-seed", 180000)
			if a, ok := v.(AudioAware); ok {
				a.SetAudioData(frame)
			}
			if a, ok := v.(ImageAware); ok {
				a.SetImage(img)
			}
			if a, ok := v.(LyricsAware); ok {
				a.SetLyrics(lyrics)
			}
			if a, ok := v.(ProgressAware); ok {
				a.SetProgress(60000)
			}
			// Warm up frame-to-frame state (trails, history, caches).
			for range 30 {
				v.Advance()
				v.View(benchW, benchH)
			}
			b.ReportAllocs()
			for b.Loop() {
				v.Advance()
				v.View(benchW, benchH)
			}
		})
	}
}
