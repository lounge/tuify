//go:build contract

// Contract tests hit the real LRCLIB API to check that its responses
// still match what the client assumes. They are excluded from the normal
// build; run them manually to detect upstream changes:
//
//	go test -tags contract ./internal/lyrics/ -run TestContract -v

package lyrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContract_LRCLIBGet_Synced(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// Bohemian Rhapsody is a stable, well-known song with synced lyrics.
	res, err := lrclibGet(ctx, contractClient, "Bohemian Rhapsody", "Queen", 354000)
	if err != nil {
		t.Fatalf("lrclibGet failed: %v", err)
	}
	if !res.Synced() {
		t.Fatalf("expected synced lyrics, got %d untimed line(s) — syncedLyrics may have changed shape", len(res.Lines))
	}
	if len(res.Lines) < 30 {
		t.Errorf("only %d line(s), expected 30+", len(res.Lines))
	}
	var text strings.Builder
	for _, l := range res.Lines {
		text.WriteString(l.Text)
		text.WriteByte('\n')
	}
	for _, phrase := range []string{"Is this the real life", "Mama", "Galileo"} {
		if !strings.Contains(text.String(), phrase) {
			t.Errorf("lyrics missing expected phrase %q", phrase)
		}
	}
}

func TestContract_LRCLIBGet_Instrumental(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// "Orion" by Metallica is a well-known instrumental.
	_, err := lrclibGet(ctx, contractClient, "Orion", "Metallica", 507000)
	if !errors.Is(err, ErrInstrumental) {
		t.Errorf("err = %v, want ErrInstrumental — LRCLIB's instrumental flag may have changed", err)
	}
}

func TestContract_LRCLIBSearch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// The suffix is stripped by improveQuery before the fuzzy search.
	res, err := lrclibSearch(ctx, contractClient, "Bohemian Rhapsody - Remastered 2011", "Queen", 354000)
	if err != nil {
		t.Fatalf("lrclibSearch failed: %v", err)
	}
	if len(res.Lines) == 0 {
		t.Fatal("lrclibSearch found nothing — the search endpoint or its parameters may have changed")
	}
}
