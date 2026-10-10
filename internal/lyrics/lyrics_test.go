package lyrics

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
)

// lyricsStub is an in-memory stand-in for both LRCLIB and Genius. Every
// request reaches the same test server, so endpoints are told apart by
// path and query: LRCLIB's search carries track_name, Genius's carries q.
// Unset handlers answer 404.
type lyricsStub struct {
	lrclibGet    http.HandlerFunc
	lrclibSearch http.HandlerFunc
	geniusSearch http.HandlerFunc
	geniusPage   http.HandlerFunc
	geniusCalls  atomic.Int32
}

func (s *lyricsStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, genius := s.geniusPage, true
	switch {
	case r.URL.Path == "/api/get":
		h, genius = s.lrclibGet, false
	case r.URL.Path == "/api/search" && r.URL.Query().Has("track_name"):
		h, genius = s.lrclibSearch, false
	case r.URL.Path == "/api/search":
		h = s.geniusSearch
	}
	if genius {
		s.geniusCalls.Add(1)
	}
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (s *lyricsStub) client() (*http.Client, func()) {
	return newTestClient(s.ServeHTTP)
}

func lrclibRecordHandler(rec map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, rec)
	}
}

func lrclibSearchHandler(recs ...map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if recs == nil {
			recs = []map[string]any{}
		}
		json.MarshalWrite(w, recs)
	}
}

func geniusStub(title, artist string) (search, page http.HandlerFunc) {
	search = func(w http.ResponseWriter, r *http.Request) {
		json.MarshalWrite(w, geniusSearchResponse([]map[string]any{
			songHit(title, artist, "https://genius.com/song-lyrics", false),
		}))
	}
	page = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<div data-lyrics-container="true">From Genius<br>Second line</div>`))
	}
	return search, page
}

func statusHandler(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	}
}

// --- Result ---

func TestResult_Synced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		res  Result
		want bool
	}{
		{"empty", Result{}, false},
		{"untimed", Result{Lines: []Line{{StartMs: -1, Text: "a"}}}, false},
		{"timed from zero", Result{Lines: []Line{{StartMs: 0, Text: "a"}}}, true},
		{"timed", Result{Lines: []Line{{StartMs: 1500, Text: "a"}}}, true},
	}
	for _, tt := range tests {
		if got := tt.res.Synced(); got != tt.want {
			t.Errorf("%s: Synced() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// --- Search ---

func TestSearch_LRCLIBExactHitSkipsGenius(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{
		lrclibGet:    lrclibRecordHandler(lrclibJSON("Artist", 200, "[00:01.00] timed", "", false)),
		geniusSearch: gs,
		geniusPage:   gp,
	}
	client, cleanup := stub.client()
	defer cleanup()

	res, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []Line{{StartMs: 1000, Text: "timed"}}; !slices.Equal(res.Lines, want) {
		t.Errorf("lines = %v, want %v", res.Lines, want)
	}
	if n := stub.geniusCalls.Load(); n != 0 {
		t.Errorf("Genius was called %d time(s) after an LRCLIB hit", n)
	}
}

func TestSearch_FallsBackToLRCLIBSearch(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{
		lrclibGet:    statusHandler(http.StatusNotFound),
		lrclibSearch: lrclibSearchHandler(lrclibJSON("Artist", 201, "[00:02.00] fuzzy", "", false)),
		geniusSearch: gs,
		geniusPage:   gp,
	}
	client, cleanup := stub.client()
	defer cleanup()

	res, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if want := []Line{{StartMs: 2000, Text: "fuzzy"}}; !slices.Equal(res.Lines, want) {
		t.Errorf("lines = %v, want %v", res.Lines, want)
	}
	if n := stub.geniusCalls.Load(); n != 0 {
		t.Errorf("Genius was called %d time(s) after an LRCLIB search hit", n)
	}
}

func TestSearch_FallsBackToGeniusOnMiss(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{
		lrclibGet:    statusHandler(http.StatusNotFound),
		lrclibSearch: lrclibSearchHandler(),
		geniusSearch: gs,
		geniusPage:   gp,
	}
	client, cleanup := stub.client()
	defer cleanup()

	res, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []Line{{StartMs: -1, Text: "From Genius"}, {StartMs: -1, Text: "Second line"}}
	if !slices.Equal(res.Lines, want) {
		t.Errorf("lines = %v, want %v", res.Lines, want)
	}
	if res.Synced() {
		t.Error("Genius lyrics reported as synced")
	}
}

// LRCLIB is a single volunteer-run service; a 5xx must not cost the user
// their lyrics.
func TestSearch_FallsBackToGeniusOnLRCLIBError(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{
		lrclibGet:    statusHandler(http.StatusServiceUnavailable),
		lrclibSearch: lrclibSearchHandler(lrclibJSON("Artist", 200, "[00:01.00] never reached", "", false)),
		geniusSearch: gs,
		geniusPage:   gp,
	}
	client, cleanup := stub.client()
	defer cleanup()

	res, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Lines) == 0 || res.Lines[0].Text != "From Genius" {
		t.Errorf("lines = %v, want Genius lyrics", res.Lines)
	}
}

func TestSearch_InstrumentalFromLRCLIBStopsEarly(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{
		lrclibGet:    lrclibRecordHandler(lrclibJSON("Artist", 200, "", "", true)),
		geniusSearch: gs,
		geniusPage:   gp,
	}
	client, cleanup := stub.client()
	defer cleanup()

	_, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if !errors.Is(err, ErrInstrumental) {
		t.Errorf("err = %v, want ErrInstrumental", err)
	}
	if n := stub.geniusCalls.Load(); n != 0 {
		t.Errorf("Genius was called %d time(s) for an instrumental", n)
	}
}

func TestSearch_GeniusErrorIsReturned(t *testing.T) {
	t.Parallel()

	stub := &lyricsStub{
		lrclibGet:    statusHandler(http.StatusNotFound),
		lrclibSearch: lrclibSearchHandler(),
		geniusSearch: statusHandler(http.StatusInternalServerError),
	}
	client, cleanup := stub.client()
	defer cleanup()

	if _, err := Search(context.Background(), client, "Song", "Artist", 200000); err == nil {
		t.Fatal("expected Genius's error to be returned")
	}
}

func TestSearch_NothingFound(t *testing.T) {
	t.Parallel()

	stub := &lyricsStub{
		lrclibGet:    statusHandler(http.StatusNotFound),
		lrclibSearch: lrclibSearchHandler(),
		geniusSearch: func(w http.ResponseWriter, r *http.Request) {
			json.MarshalWrite(w, geniusSearchResponse(nil))
		},
	}
	client, cleanup := stub.client()
	defer cleanup()

	res, err := Search(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Lines) != 0 {
		t.Errorf("lines = %v, want none", res.Lines)
	}
}

// A cancelled context fails the LRCLIB request; that is not a reason to
// try Genius.
func TestSearch_CancelledContextDoesNotTryGenius(t *testing.T) {
	t.Parallel()

	gs, gp := geniusStub("Song", "Artist")
	stub := &lyricsStub{geniusSearch: gs, geniusPage: gp}
	client, cleanup := stub.client()
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Search(ctx, client, "Song", "Artist", 200000)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if n := stub.geniusCalls.Load(); n != 0 {
		t.Errorf("Genius was called %d time(s) with a cancelled context", n)
	}
}

// --- plainLines ---

func TestPlainLines(t *testing.T) {
	t.Parallel()

	if got := plainLines(""); got != nil {
		t.Errorf("plainLines(\"\") = %v, want nil", got)
	}
	want := []Line{{StartMs: -1, Text: "a"}, {StartMs: -1, Text: ""}, {StartMs: -1, Text: "b"}}
	if got := plainLines("a\n\nb"); !slices.Equal(got, want) {
		t.Errorf("plainLines = %v, want %v", got, want)
	}
}

func TestArtistMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		hit, artist string
		want        bool
	}{
		{"Queen", "Queen", true},
		{"queen, david bowie", "Queen", true},
		{"Guns N’ Roses", "Guns N' Roses", true},
		{"Art", "Artist", false},
		{"Cover Band", "Artist", false},
		{"Some Artist", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		if got := artistMatches(tt.hit, tt.artist); got != tt.want {
			t.Errorf("artistMatches(%q, %q) = %v, want %v", tt.hit, tt.artist, got, tt.want)
		}
	}
}
