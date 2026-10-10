package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// lrclibJSON builds one LRCLIB record as the API returns it. Empty synced
// or plain lyrics are encoded as JSON null, like the real service does.
func lrclibJSON(artist string, duration float64, synced, plain string, instrumental bool) map[string]any {
	rec := map[string]any{
		"id":           1,
		"trackName":    "Song",
		"artistName":   artist,
		"albumName":    "Album",
		"duration":     duration,
		"instrumental": instrumental,
		"plainLyrics":  nil,
		"syncedLyrics": nil,
	}
	if synced != "" {
		rec["syncedLyrics"] = synced
	}
	if plain != "" {
		rec["plainLyrics"] = plain
	}
	return rec
}

// --- lrclibGet ---

func TestLRCLIBGet_SyncedLyrics(t *testing.T) {
	t.Parallel()

	var gotPath, gotUA string
	var gotQuery map[string][]string
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUA, gotQuery = r.URL.Path, r.Header.Get("User-Agent"), r.URL.Query()
		json.NewEncoder(w).Encode(lrclibJSON("Queen", 355, "[00:00.15] Is this the real life?\n[00:07.13] Caught in a landslide", "x", false))
	})
	defer cleanup()

	res, err := lrclibGet(context.Background(), client, "Bohemian Rhapsody", "Queen", 354400)
	if err != nil {
		t.Fatalf("lrclibGet: %v", err)
	}
	want := []Line{{StartMs: 150, Text: "Is this the real life?"}, {StartMs: 7130, Text: "Caught in a landslide"}}
	if !res.Synced() || len(res.Lines) != 2 || res.Lines[0] != want[0] || res.Lines[1] != want[1] {
		t.Errorf("lines = %v, want %v", res.Lines, want)
	}
	if gotPath != "/api/get" {
		t.Errorf("path = %q, want /api/get", gotPath)
	}
	if gotUA != userAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, userAgent)
	}
	for k, want := range map[string]string{"artist_name": "Queen", "track_name": "Bohemian Rhapsody", "duration": "354"} {
		if got := gotQuery[k]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %v, want %q", k, got, want)
		}
	}
}

// Only plain lyrics uploaded: the record has syncedLyrics null. The text
// goes through the same normalization as Genius text.
func TestLRCLIBGet_PlainLyricsOnly(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lrclibJSON("Artist", 200, "", "  First\n\n\n\nSecond  \n", false))
	})
	defer cleanup()

	res, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("lrclibGet: %v", err)
	}
	want := []Line{{StartMs: -1, Text: "First"}, {StartMs: -1, Text: ""}, {StartMs: -1, Text: "Second"}}
	if res.Synced() || len(res.Lines) != 3 || res.Lines[0] != want[0] || res.Lines[1] != want[1] || res.Lines[2] != want[2] {
		t.Errorf("lines = %v, want %v", res.Lines, want)
	}
}

// Synced text that parses to no lines (metadata only) falls back to plain.
func TestLRCLIBGet_UnparsableSyncedFallsBackToPlain(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lrclibJSON("Artist", 200, "[ar:Artist]\n[ti:Song]", "Plain text", false))
	})
	defer cleanup()

	res, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("lrclibGet: %v", err)
	}
	if res.Synced() || len(res.Lines) != 1 || res.Lines[0].Text != "Plain text" {
		t.Errorf("lines = %v, want the plain text", res.Lines)
	}
}

func TestLRCLIBGet_NotFoundIsAMiss(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Failed to find specified track","name":"TrackNotFound","statusCode":404}`))
	})
	defer cleanup()

	res, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("404 must not be an error, got %v", err)
	}
	if len(res.Lines) != 0 {
		t.Errorf("lines = %v, want none", res.Lines)
	}
}

func TestLRCLIBGet_ServerErrorIsAnError(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	defer cleanup()

	if _, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000); err == nil {
		t.Fatal("expected an error for 503")
	}
}

func TestLRCLIBGet_Instrumental(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lrclibJSON("Metallica", 507, "", "", true))
	})
	defer cleanup()

	_, err := lrclibGet(context.Background(), client, "Orion", "Metallica", 507000)
	if !errors.Is(err, ErrInstrumental) {
		t.Errorf("err = %v, want ErrInstrumental", err)
	}
}

// An unknown duration is left out rather than sent as 0, which would
// never be within tolerance of anything.
func TestLRCLIBGet_OmitsUnknownDuration(t *testing.T) {
	t.Parallel()

	var hasDuration atomic.Bool
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		hasDuration.Store(r.URL.Query().Has("duration"))
		w.WriteHeader(http.StatusNotFound)
	})
	defer cleanup()

	if _, err := lrclibGet(context.Background(), client, "Song", "Artist", 0); err != nil {
		t.Fatalf("lrclibGet: %v", err)
	}
	if hasDuration.Load() {
		t.Error("duration=0 was sent")
	}
}

// LRC text is crowd-uploaded; control characters must not reach the
// terminal.
func TestLRCLIBGet_StripsTerminalEscapes(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(lrclibJSON("Artist", 200, "[00:01.00] a\x1b]52;c;SGVsbG8=\x07b", "", false))
	})
	defer cleanup()

	res, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("lrclibGet: %v", err)
	}
	if got := res.Lines[0].Text; strings.ContainsAny(got, "\x1b\x07") || got != "a]52;c;SGVsbG8=b" {
		t.Errorf("text = %q, escapes survived", got)
	}
}

// A record that never ends must fail at maxBodyBytes rather than being
// decoded from an unbounded buffer.
func TestLRCLIBGet_OversizeBodyIsCutOff(t *testing.T) {
	t.Parallel()

	var served atomic.Int64
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		served.Store(streamUntilClosed(w, `{"instrumental":false,"syncedLyrics":"`, strings.Repeat("[00:01.00] la la la\\n", 1024)))
	})
	defer cleanup()

	if _, err := lrclibGet(context.Background(), client, "Song", "Artist", 200000); err == nil {
		t.Fatal("lrclibGet decoded an endless response without error")
	}
	if n := served.Load(); n > 3*maxBodyBytes {
		t.Errorf("server pushed %d bytes; the body was not cut off near %d", n, maxBodyBytes)
	}
}

// --- lrclibSearch ---

func TestLRCLIBSearch_CleansTitleAndPicksByDuration(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery map[string][]string
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		json.NewEncoder(w).Encode([]map[string]any{
			lrclibJSON("Queen", 317, "[00:01.00] soundtrack cut", "", false),
			lrclibJSON("Queen", 355, "[00:01.00] album cut", "", false),
			lrclibJSON("Queen", 263, "[00:01.00] radio edit", "", false),
		})
	})
	defer cleanup()

	res, err := lrclibSearch(context.Background(), client, "Bohemian Rhapsody - Remastered 2011", "Queen", 354000)
	if err != nil {
		t.Fatalf("lrclibSearch: %v", err)
	}
	if len(res.Lines) != 1 || res.Lines[0].Text != "album cut" {
		t.Errorf("lines = %v, want the 355 s hit", res.Lines)
	}
	if gotPath != "/api/search" {
		t.Errorf("path = %q, want /api/search", gotPath)
	}
	if got := gotQuery["track_name"]; len(got) != 1 || got[0] != "Bohemian Rhapsody" {
		t.Errorf("track_name = %v, want the cleaned title", got)
	}
	if got := gotQuery["artist_name"]; len(got) != 1 || got[0] != "Queen" {
		t.Errorf("artist_name = %v, want Queen", got)
	}
}

func TestLRCLIBSearch_NoHitInToleranceIsAMiss(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			lrclibJSON("Artist", 180, "[00:01.00] x", "", false),
		})
	})
	defer cleanup()

	res, err := lrclibSearch(context.Background(), client, "Song", "Artist", 200000)
	if err != nil {
		t.Fatalf("lrclibSearch: %v", err)
	}
	if len(res.Lines) != 0 {
		t.Errorf("lines = %v, want none", res.Lines)
	}
}

func TestLRCLIBSearch_Instrumental(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{lrclibJSON("Artist", 200, "", "", true)})
	})
	defer cleanup()

	_, err := lrclibSearch(context.Background(), client, "Song", "Artist", 200000)
	if !errors.Is(err, ErrInstrumental) {
		t.Errorf("err = %v, want ErrInstrumental", err)
	}
}

func TestLRCLIBSearch_ServerErrorIsAnError(t *testing.T) {
	t.Parallel()

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()

	if _, err := lrclibSearch(context.Background(), client, "Song", "Artist", 200000); err == nil {
		t.Fatal("expected an error for 500")
	}
}

// --- pickLRCLIBHit ---

func TestPickLRCLIBHit(t *testing.T) {
	t.Parallel()

	synced := func(artist string, dur float64) lrclibRecord {
		return lrclibRecord{ArtistName: artist, Duration: dur, SyncedLyrics: "[00:01.00] x"}
	}
	plain := func(artist string, dur float64) lrclibRecord {
		return lrclibRecord{ArtistName: artist, Duration: dur, PlainLyrics: "x"}
	}
	instrumental := func(artist string, dur float64) lrclibRecord {
		return lrclibRecord{ArtistName: artist, Duration: dur, Instrumental: true}
	}

	tests := []struct {
		name       string
		recs       []lrclibRecord
		artist     string
		durationMs int
		want       int // index into recs, or -1 for nil
	}{
		{"first hit in tolerance", []lrclibRecord{synced("Artist", 100), synced("Artist", 200)}, "Artist", 200000, 1},
		{"tolerance is inclusive at 2 s", []lrclibRecord{synced("Artist", 202)}, "Artist", 200000, 0},
		{"just outside tolerance", []lrclibRecord{synced("Artist", 202.5)}, "Artist", 200000, -1},
		{"synced beats earlier plain", []lrclibRecord{plain("Artist", 200), synced("Artist", 201)}, "Artist", 200000, 1},
		{"plain beats earlier instrumental", []lrclibRecord{instrumental("Artist", 200), plain("Artist", 200)}, "Artist", 200000, 1},
		{"first wins a tie", []lrclibRecord{synced("Artist", 200), synced("Artist", 200)}, "Artist", 200000, 0},
		{"instrumental alone is a hit", []lrclibRecord{instrumental("Artist", 200)}, "Artist", 200000, 0},
		{"record with nothing is skipped", []lrclibRecord{{ArtistName: "Artist", Duration: 200}}, "Artist", 200000, -1},
		{"other artist is skipped", []lrclibRecord{synced("Cover Band", 200), synced("Artist", 200)}, "Artist", 200000, 1},
		{"hit naming only part of our artist is skipped", []lrclibRecord{synced("Art", 200)}, "Artist", 200000, -1},
		{"artist match is case-insensitive and partial", []lrclibRecord{synced("queen, david bowie", 200)}, "Queen", 200000, 0},
		{"typographic apostrophes match ascii", []lrclibRecord{synced("Guns N’ Roses", 200)}, "Guns N' Roses", 200000, 0},
		{"unknown duration accepts any length", []lrclibRecord{synced("Artist", 999)}, "Artist", 0, 0},
		{"no records", nil, "Artist", 200000, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pickLRCLIBHit(tt.recs, tt.artist, tt.durationMs)
			switch {
			case tt.want < 0 && got != nil:
				t.Errorf("got %+v, want nil", *got)
			case tt.want >= 0 && got == nil:
				t.Errorf("got nil, want recs[%d]", tt.want)
			case tt.want >= 0 && got != &tt.recs[tt.want]:
				t.Errorf("got %+v, want recs[%d]", *got, tt.want)
			}
		})
	}
}

func TestRoundSeconds(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ ms, want int }{{0, 0}, {354400, 354}, {354500, 355}, {999, 1}, {499, 0}} {
		if got := roundSeconds(tt.ms); got != tt.want {
			t.Errorf("roundSeconds(%d) = %d, want %d", tt.ms, got, tt.want)
		}
	}
}
