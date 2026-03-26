package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// ErrInstrumental is returned when Genius marks a song as instrumental.
var ErrInstrumental = errors.New("instrumental")

var (
	reRemix  = regexp.MustCompile(`(?i)\s*[-–—]\s*(feat\.?|ft\.?|remix|remaster(ed)?|deluxe|bonus|live|acoustic|version|edit|mix|radio)\b.*$`)
	reParens = regexp.MustCompile(`(?i)\s*\([^)]*?(feat\.?|ft\.?|remix|remaster(ed)?|deluxe|bonus|live|acoustic|version|edit|mix|radio)[^)]*?\)`)
)

// Search finds lyrics for a track on Genius.
// Returns the lyrics text or an error. Returns empty string if no lyrics found.
// Returns ErrInstrumental if Genius marks the song as instrumental.
// The context can be used to cancel or set a deadline on the search.
func Search(ctx context.Context, client *http.Client, track, artist string) (string, error) {
	query := improveQuery(artist + " " + track)
	result, err := searchSong(ctx, client, query, track, artist)
	if err != nil {
		return "", err
	}
	if result.url == "" {
		return "", nil
	}
	if result.instrumental {
		return "", ErrInstrumental
	}
	return scrapeLyrics(ctx, client, result.url)
}

func improveQuery(s string) string {
	s = reParens.ReplaceAllString(s, "")
	s = reRemix.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", " ")
	return strings.TrimSpace(s)
}

type songResult struct {
	url          string
	instrumental bool
}

func searchSong(ctx context.Context, client *http.Client, query, track, artist string) (songResult, error) {
	endpoint := "https://genius.com/api/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return songResult{}, fmt.Errorf("genius search: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return songResult{}, fmt.Errorf("genius search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return songResult{}, fmt.Errorf("genius search: status %d", resp.StatusCode)
	}

	var result struct {
		Meta struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"meta"`
		Response struct {
			Hits []struct {
				Type   string `json:"type"`
				Result struct {
					URL                string `json:"url"`
					Title              string `json:"title"`
					ArtistNames        string `json:"artist_names"`
					PrimaryArtistNames string `json:"primary_artist_names"`
					Instrumental       bool   `json:"instrumental"`
				} `json:"result"`
			} `json:"hits"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return songResult{}, fmt.Errorf("genius search: %w", err)
	}
	if result.Meta.Status != 200 {
		msg := result.Meta.Message
		if msg == "" {
			msg = fmt.Sprintf("status %d", result.Meta.Status)
		}
		return songResult{}, fmt.Errorf("genius search: %s", msg)
	}
	artistLower := strings.ToLower(artist)
	trackClean := strings.ToLower(improveQuery(track))
	for _, hit := range result.Response.Hits {
		if hit.Type != "song" {
			continue
		}
		if strings.Contains(hit.Result.ArtistNames, "Genius") {
			continue
		}
		if !strings.Contains(strings.ToLower(hit.Result.ArtistNames), artistLower) &&
			!strings.Contains(strings.ToLower(hit.Result.PrimaryArtistNames), artistLower) {
			continue
		}
		titleClean := strings.ToLower(improveQuery(hit.Result.Title))
		if !strings.Contains(titleClean, trackClean) && !strings.Contains(trackClean, titleClean) {
			continue
		}
		return songResult{url: hit.Result.URL, instrumental: hit.Result.Instrumental}, nil
	}
	return songResult{}, nil
}

func scrapeLyrics(ctx context.Context, client *http.Client, songURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, songURL, nil)
	if err != nil {
		return "", fmt.Errorf("genius fetch: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("genius fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("genius fetch: status %d", resp.StatusCode)
	}
	return extractLyrics(resp.Body)
}

func extractLyrics(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("genius parse: %w", err)
	}

	var parts []string
	var findContainers func(*html.Node)
	findContainers = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "data-lyrics-container" && attr.Val == "true" {
					parts = append(parts, extractText(n))
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findContainers(c)
		}
	}
	findContainers(doc)

	if len(parts) == 0 {
		return "", nil
	}
	return normalizeLyrics(strings.Join(parts, "\n")), nil
}

func extractText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if hasAttr(n, "data-exclude-from-selection", "true") {
				return
			}
			if n.Data == "br" {
				buf.WriteString("\n")
			}
		}
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return buf.String()
}

func hasAttr(n *html.Node, key, val string) bool {
	for _, a := range n.Attr {
		if a.Key == key && a.Val == val {
			return true
		}
	}
	return false
}

func normalizeLyrics(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		out = append(out, line)
	}
	// Collapse multiple blank lines into one.
	var result []string
	prevBlank := false
	for _, line := range out {
		if line == "" {
			if !prevBlank {
				result = append(result, "")
			}
			prevBlank = true
		} else {
			prevBlank = false
			result = append(result, line)
		}
	}
	// Trim leading/trailing blank lines.
	for len(result) > 0 && result[0] == "" {
		result = result[1:]
	}
	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}
	// Ensure exactly one blank line before section markers like [Verse].
	joined := strings.Join(result, "\n")
	joined = strings.ReplaceAll(joined, "\n\n[", "\n[")
	joined = strings.ReplaceAll(joined, "\n[", "\n\n[")
	return joined
}
