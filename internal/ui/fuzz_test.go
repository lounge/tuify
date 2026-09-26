package ui

import (
	"strings"
	"testing"
)

var prefixLetters = map[searchPrefix]string{
	prefixTrack:   "t",
	prefixEpisode: "e",
	prefixAlbum:   "l",
	prefixArtist:  "a",
	prefixShow:    "s",
}

func FuzzParseSearch(f *testing.F) {
	for _, s := range []string{"", "queen", "a:queen", "t:", ":x", "x:y", "l:a:b", "é:ü", "s: pod"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		prefix, term := parseSearch(input)
		if !strings.HasSuffix(input, term) {
			t.Fatalf("parseSearch(%q) term %q is not a suffix of the input", input, term)
		}
		letter, ok := prefixLetters[prefix]
		if !ok {
			t.Fatalf("parseSearch(%q) returned unknown prefix %d", input, prefix)
		}
		if term == input {
			// Unrecognised (or absent) prefix: the whole input is a track search.
			if prefix != prefixTrack {
				t.Fatalf("parseSearch(%q) kept the whole input but prefix = %d", input, prefix)
			}
			return
		}
		// A recognised prefix strips exactly "<letter>:".
		if input != letter+":"+term {
			t.Fatalf("parseSearch(%q) = (%s, %q); input is not %q+term", input, letter, term, letter+":")
		}
		// Round trip: re-prefixing the term parses back to the same pair.
		if p2, t2 := parseSearch(letter + ":" + term); p2 != prefix || t2 != term {
			t.Fatalf("round trip of %q gave (%d, %q), want (%d, %q)", input, p2, t2, prefix, term)
		}
	})
}

func FuzzIDFromURI(f *testing.F) {
	for _, s := range []string{"", "spotify:track:abc", "spotify:track:", "notauri", ":", "a:b:c:d"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, uri string) {
		id := idFromURI(uri)
		if !strings.HasSuffix(uri, id) {
			t.Fatalf("idFromURI(%q) = %q, not a suffix", uri, id)
		}
		if strings.Contains(id, ":") {
			t.Fatalf("idFromURI(%q) = %q contains a colon", uri, id)
		}
		if !strings.Contains(uri, ":") && id != uri {
			t.Fatalf("idFromURI(%q) = %q, want the input unchanged without a colon", uri, id)
		}
		// Whatever precedes the id is exactly the last colon.
		if strings.Contains(uri, ":") && !strings.HasSuffix(uri, ":"+id) {
			t.Fatalf("idFromURI(%q) = %q is not the text after the last colon", uri, id)
		}
	})
}

func FuzzSpotifyURL(f *testing.F) {
	for _, s := range []string{"", "spotify:track:abc", "spotify:track", "a:b:c:d", "::", "spotify:playlist:x"} {
		f.Add(s)
	}
	const base = "https://open.spotify.com/"
	f.Fuzz(func(t *testing.T, uri string) {
		url := spotifyURL(uri)
		if strings.Count(uri, ":") < 2 {
			if url != "" {
				t.Fatalf("spotifyURL(%q) = %q, want empty for fewer than two colons", uri, url)
			}
			return
		}
		if !strings.HasPrefix(url, base) {
			t.Fatalf("spotifyURL(%q) = %q, missing %q", uri, url, base)
		}
		// The scheme segment is dropped; type and id are joined with "/".
		_, rest, _ := strings.Cut(uri, ":")
		kind, id, _ := strings.Cut(rest, ":")
		if url != base+kind+"/"+id {
			t.Fatalf("spotifyURL(%q) = %q, want %q", uri, url, base+kind+"/"+id)
		}
		// For well-formed URIs (id without colons) the URL ends in idFromURI.
		if !strings.Contains(id, ":") && !strings.HasSuffix(url, "/"+idFromURI(uri)) {
			t.Fatalf("spotifyURL(%q) = %q does not end in the URI's id", uri, url)
		}
	})
}
