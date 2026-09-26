//go:build contract

// Contract tests hit the real Genius API to check that its search API and
// lyrics HTML still match what the scraper assumes. They are excluded from
// the normal build; run them manually to detect upstream changes:
//
//	go test -tags contract ./internal/lyrics/ -run TestContract -v

package lyrics

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// contractClient bounds every real request, unlike http.DefaultClient.
var contractClient = &http.Client{Timeout: 15 * time.Second}

func TestContract_SearchAndScrape(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// Bohemian Rhapsody is a stable, well-known song unlikely to be removed.
	text, err := Search(ctx, contractClient, "Bohemian Rhapsody", "Queen")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if text == "" {
		t.Fatal("Search returned empty lyrics — Genius HTML structure may have changed (data-lyrics-container attribute)")
	}

	// Verify some well-known lyrics are present.
	for _, phrase := range []string{
		"Is this the real life",
		"Mama",
		"Galileo",
	} {
		if !strings.Contains(text, phrase) {
			t.Errorf("lyrics missing expected phrase %q", phrase)
		}
	}

	// Sanity check: lyrics should be substantial (Bohemian Rhapsody is ~370 words).
	lines := strings.Split(text, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty < 30 {
		t.Errorf("lyrics seem too short (%d non-empty lines), expected 30+", nonEmpty)
	}
}

func TestContract_SearchAPI_ReturnsResults(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	result, err := searchSong(ctx, contractClient, "Queen Bohemian Rhapsody", "Bohemian Rhapsody", "Queen")
	if err != nil {
		t.Fatalf("searchSong failed: %v", err)
	}
	if result.url == "" {
		t.Fatal("searchSong returned no URL — Genius search API may have changed")
	}
	if !strings.Contains(result.url, "genius.com") {
		t.Errorf("unexpected URL: %q", result.url)
	}
}

func TestContract_HTMLStructure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// Fetch a known lyrics page directly and verify the DOM contract.
	result, err := searchSong(ctx, contractClient, "Queen Bohemian Rhapsody", "Bohemian Rhapsody", "Queen")
	if err != nil {
		t.Fatalf("searchSong failed: %v", err)
	}
	if result.url == "" {
		t.Skip("no URL found, cannot test HTML structure")
	}

	text, err := scrapeLyrics(ctx, contractClient, result.url)
	if err != nil {
		t.Fatalf("scrapeLyrics failed: %v", err)
	}
	if text == "" {
		t.Fatal("scrapeLyrics returned empty — data-lyrics-container attribute may have been renamed or removed")
	}
}

func TestContract_Instrumental(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// "Orion" by Metallica is a well-known instrumental.
	_, err := Search(ctx, contractClient, "Orion", "Metallica")
	if errors.Is(err, ErrInstrumental) {
		// Expected — Genius correctly marks it as instrumental.
		return
	}
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	// If we get here, Genius didn't mark it as instrumental. That's not
	// necessarily a contract failure (their metadata could change), so
	// just log it.
	t.Log("Orion by Metallica was not marked as instrumental — Genius metadata may have changed")
}
