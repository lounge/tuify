package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// encodeColorPNG encodes a w×h PNG of colour c, for art that has to show
// against the pane's background.
func encodeColorPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchImage(t *testing.T) {
	small := encodePNG(t, 64, 64)
	// Compresses to a few KB, well under the byte cap, but would make the
	// decoder allocate width*height up front.
	huge := encodePNG(t, maxAlbumArtSide+1, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small.png":
			w.Write(small)
		case "/huge.png":
			w.Write(huge)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		path    string
		wantErr bool
	}{
		{"/small.png", false},
		{"/huge.png", true},
		{"/missing.png", true},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			res := fetchImage(t.Context(), srv.Client(), srv.URL+tc.path)
			if (res.err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", res.err, tc.wantErr)
			}
			if !tc.wantErr && res.img.Bounds().Dx() != 64 {
				t.Errorf("decoded width %d, want 64", res.img.Bounds().Dx())
			}
		})
	}
}
