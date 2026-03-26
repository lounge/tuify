package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rewriteTransport redirects all requests to the test server,
// so hardcoded genius.com URLs resolve locally.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	return t.base.RoundTrip(req)
}

func newTestClient(handler http.HandlerFunc) (*http.Client, func()) {
	srv := httptest.NewServer(handler)
	transport := &rewriteTransport{base: srv.Client().Transport, target: srv.URL}
	return &http.Client{Transport: transport}, srv.Close
}

// geniusSearchResponse builds a Genius API search response JSON.
func geniusSearchResponse(hits []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"meta": map[string]interface{}{"status": 200},
		"response": map[string]interface{}{
			"hits": hits,
		},
	}
}

func songHit(title, artistNames, url string, instrumental bool) map[string]interface{} {
	return map[string]interface{}{
		"type": "song",
		"result": map[string]interface{}{
			"title":                title,
			"artist_names":         artistNames,
			"primary_artist_names": artistNames,
			"url":                  url,
			"instrumental":         instrumental,
		},
	}
}

// --- improveQuery ---

func TestImproveQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Song Title (feat. Artist)", "Song Title"},
		{"Song Title (Remix)", "Song Title"},
		{"Song Title (Deluxe Edition)", "Song Title"},
		{"Song Title - Remastered", "Song Title"},
		{"Song Title – Live Version", "Song Title"},
		{"Artist & Other", "Artist   Other"},
		{"Clean Song", "Clean Song"},
		{"Song (Acoustic Version) - Remastered 2023", "Song"},
	}
	for _, tt := range tests {
		got := improveQuery(tt.input)
		if got != tt.want {
			t.Errorf("improveQuery(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- normalizeLyrics ---

func TestNormalizeLyrics(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "collapses multiple blank lines",
			input: "line one\n\n\n\nline two",
			want:  "line one\n\nline two",
		},
		{
			name:  "trims leading and trailing blanks",
			input: "\n\nline one\nline two\n\n",
			want:  "line one\nline two",
		},
		{
			name:  "adds blank line before section markers",
			input: "line one\n[Chorus]\nline two",
			want:  "line one\n\n[Chorus]\nline two",
		},
		{
			name:  "does not double blank before section markers",
			input: "line one\n\n[Chorus]\nline two",
			want:  "line one\n\n[Chorus]\nline two",
		},
		{
			name:  "trims whitespace from lines",
			input: "  hello  \n  world  ",
			want:  "hello\nworld",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLyrics(tt.input)
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// --- extractLyrics ---

func TestExtractLyrics(t *testing.T) {
	html := `<html><body>
		<div data-lyrics-container="true">Hello<br>World</div>
		<div>not lyrics</div>
		<div data-lyrics-container="true">Second verse</div>
	</body></html>`

	got, err := extractLyrics(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("missing first container text: %q", got)
	}
	if !strings.Contains(got, "Second verse") {
		t.Errorf("missing second container text: %q", got)
	}
	if strings.Contains(got, "not lyrics") {
		t.Errorf("should not include non-lyrics div: %q", got)
	}
}

func TestExtractLyrics_ExcludesSelection(t *testing.T) {
	html := `<html><body>
		<div data-lyrics-container="true">
			Keep this
			<span data-exclude-from-selection="true">Remove this</span>
		</div>
	</body></html>`

	got, err := extractLyrics(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Keep this") {
		t.Errorf("missing kept text: %q", got)
	}
	if strings.Contains(got, "Remove this") {
		t.Errorf("should exclude selection-excluded text: %q", got)
	}
}

func TestExtractLyrics_NoContainers(t *testing.T) {
	html := `<html><body><div>no lyrics here</div></body></html>`

	got, err := extractLyrics(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- searchSong ---

func TestSearchSong_MatchesCorrectHit(t *testing.T) {
	resp := geniusSearchResponse([]map[string]interface{}{
		songHit("Wrong Song", "Wrong Artist", "https://genius.com/wrong", false),
		songHit("Right Song", "Right Artist", "https://genius.com/right", false),
	})

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	result, err := searchSong(context.Background(), client, "right artist right song", "Right Song", "Right Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.url != "https://genius.com/right" {
		t.Errorf("got url %q, want https://genius.com/right", result.url)
	}
}

func TestSearchSong_SkipsGeniusAnnotations(t *testing.T) {
	resp := geniusSearchResponse([]map[string]interface{}{
		songHit("Song", "Genius English Translations", "https://genius.com/genius", false),
		songHit("Song", "Real Artist", "https://genius.com/real", false),
	})

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	result, err := searchSong(context.Background(), client, "real artist song", "Song", "Real Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.url != "https://genius.com/real" {
		t.Errorf("got url %q, want https://genius.com/real", result.url)
	}
}

func TestSearchSong_SkipsNonSongTypes(t *testing.T) {
	resp := geniusSearchResponse([]map[string]interface{}{
		{"type": "article", "result": map[string]interface{}{
			"title": "Song", "artist_names": "Artist", "primary_artist_names": "Artist",
			"url": "https://genius.com/article", "instrumental": false,
		}},
		songHit("Song", "Artist", "https://genius.com/song", false),
	})

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	result, err := searchSong(context.Background(), client, "artist song", "Song", "Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.url != "https://genius.com/song" {
		t.Errorf("got url %q, want https://genius.com/song", result.url)
	}
}

func TestSearchSong_Instrumental(t *testing.T) {
	resp := geniusSearchResponse([]map[string]interface{}{
		songHit("Song", "Artist", "https://genius.com/song", true),
	})

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	result, err := searchSong(context.Background(), client, "artist song", "Song", "Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.instrumental {
		t.Error("expected instrumental=true")
	}
}

func TestSearchSong_NoMatch(t *testing.T) {
	resp := geniusSearchResponse([]map[string]interface{}{
		songHit("Completely Different", "Unknown", "https://genius.com/nope", false),
	})

	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	result, err := searchSong(context.Background(), client, "artist song", "Song", "Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.url != "" {
		t.Errorf("expected empty url for no match, got %q", result.url)
	}
}

func TestSearchSong_APIError(t *testing.T) {
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer cleanup()

	_, err := searchSong(context.Background(), client, "query", "Song", "Artist")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

// --- Search (end-to-end) ---

func TestSearch_EndToEnd(t *testing.T) {
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/search") {
			resp := geniusSearchResponse([]map[string]interface{}{
				songHit("My Song", "The Artist", "https://genius.com/the-artist-my-song-lyrics", false),
			})
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Lyrics page
		w.Write([]byte(`<html><body>
			<div data-lyrics-container="true">First line<br>Second line</div>
		</body></html>`))
	})
	defer cleanup()

	text, err := Search(context.Background(), client, "My Song", "The Artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "First line") || !strings.Contains(text, "Second line") {
		t.Errorf("unexpected lyrics: %q", text)
	}
}

func TestSearch_Instrumental(t *testing.T) {
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		resp := geniusSearchResponse([]map[string]interface{}{
			songHit("Instrumental Track", "Artist", "https://genius.com/inst", true),
		})
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	_, err := Search(context.Background(), client, "Instrumental Track", "Artist")
	if err != ErrInstrumental {
		t.Errorf("expected ErrInstrumental, got %v", err)
	}
}

func TestSearch_NoResults(t *testing.T) {
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		resp := geniusSearchResponse(nil)
		json.NewEncoder(w).Encode(resp)
	})
	defer cleanup()

	text, err := Search(context.Background(), client, "Unknown Song", "Nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

func TestSearch_CaseInsensitiveMatch(t *testing.T) {
	client, cleanup := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/search") {
			resp := geniusSearchResponse([]map[string]interface{}{
				songHit("MY SONG", "THE ARTIST", "https://genius.com/lyrics", false),
			})
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.Write([]byte(`<html><body>
			<div data-lyrics-container="true">Lyrics here</div>
		</body></html>`))
	})
	defer cleanup()

	text, err := Search(context.Background(), client, "my song", "the artist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "Lyrics here") {
		t.Errorf("unexpected lyrics: %q", text)
	}
}
