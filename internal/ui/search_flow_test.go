package ui

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// searchStub serves /v1/search (artists and tracks) and
// /v1/artists/{id}/albums as paginated catalogues of `total` rows each.
type searchStub struct {
	total    int
	requests atomic.Int32
}

func (s *searchStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	end := min(offset+limit, s.total)

	var items []map[string]any
	for i := offset; i < end; i++ {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/artists/"):
			artist := strings.Split(r.URL.Path, "/")[3]
			id := fmt.Sprintf("%s-album%d", artist, i)
			items = append(items, map[string]any{"id": id, "uri": "spotify:album:" + id, "name": "Album " + id})
		case q.Get("type") == "artist":
			id := fmt.Sprintf("artist%d", i)
			items = append(items, map[string]any{"id": id, "uri": "spotify:artist:" + id, "name": "Artist " + id})
		default:
			id := fmt.Sprintf("track%d", i)
			items = append(items, map[string]any{"id": id, "uri": "spotify:track:" + id, "name": "Track " + id})
		}
	}
	page := map[string]any{"offset": offset, "total": s.total, "items": items}

	var body any = page
	if r.URL.Path == "/v1/search" {
		body = map[string]any{q.Get("type") + "s": page}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, body)
}

func newStubSearchView(t *testing.T, total int) (*searchView, *searchStub) {
	t.Helper()
	stub := &searchStub{total: total}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	client := spotify.New(&http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}})
	return newSearchView(t.Context(), client, 80, 20, false), stub
}

// typeQuery types s one rune at a time through the view's search input,
// returning the Cmd produced by the last keystroke.
func typeQuery(t *testing.T, v *searchView, s string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range s {
		sc, ok := v.activeSearchInput()
		if !ok {
			t.Fatal("search input is not active")
		}
		var handled bool
		cmd, handled = handleSearchKey(sc, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if !handled {
			t.Fatalf("key %q not handled by the search input", r)
		}
	}
	return cmd
}

// commitSearch fires the debounce for the current query and applies the
// first page it fetches.
func commitSearch(t *testing.T, v *searchView) {
	t.Helper()
	cmd := v.Update(searchDebounceMsg{seq: v.debounceSeq, query: v.searchQuery})
	if cmd == nil {
		t.Fatal("current debounce did not start a fetch")
	}
	applySearchResult(t, v, cmd)
}

func applySearchResult(t *testing.T, v *searchView, cmd tea.Cmd) {
	t.Helper()
	res, ok := cmd().(searchResultMsg)
	if !ok {
		t.Fatal("fetch Cmd did not produce a searchResultMsg")
	}
	if res.err != nil {
		t.Fatalf("stubbed fetch failed: %v", res.err)
	}
	v.Update(res)
}

func uris(items []list.Item) []string {
	var out []string
	for _, it := range items {
		if u, ok := it.(uriItem); ok {
			out = append(out, u.URI())
		}
	}
	return out
}

func TestSearchView_TypingDebouncesFromTwoRunes(t *testing.T) {
	v, _ := newStubSearchView(t, 5)

	if cmd := typeQuery(t, v, "q"); cmd != nil {
		t.Error("a one-rune term must not schedule a debounce")
	}
	if cmd := typeQuery(t, v, "u"); cmd == nil {
		t.Error("a two-rune term must schedule a debounce")
	}
	if v.debounceSeq != 2 {
		t.Errorf("debounceSeq = %d, want 2 (bumped on every edit)", v.debounceSeq)
	}

	// The prefix doesn't count toward the two-rune minimum.
	v2, _ := newStubSearchView(t, 5)
	if cmd := typeQuery(t, v2, "a:q"); cmd != nil {
		t.Error(`"a:q" has a one-rune term; it must not schedule a debounce`)
	}
}

func TestSearchView_OnlyLatestDebounceFetches(t *testing.T) {
	v, stub := newStubSearchView(t, 5)
	typeQuery(t, v, "queen")
	if v.debounceSeq != 5 {
		t.Fatalf("setup: debounceSeq = %d, want 5", v.debounceSeq)
	}

	for seq := 1; seq < 5; seq++ {
		if cmd := v.Update(searchDebounceMsg{seq: seq, query: "quee"[:seq]}); cmd != nil {
			t.Errorf("stale debounce seq %d started a fetch", seq)
		}
	}
	if v.pending != 0 || v.query != "" {
		t.Errorf("stale debounce changed state: pending=%d query=%q", v.pending, v.query)
	}

	commitSearch(t, v)
	if v.query != "queen" || v.prefix != prefixTrack {
		t.Errorf("committed query = %q prefix %v, want queen/track", v.query, v.prefix)
	}
	if got := len(uris(v.items)); got != 5 {
		t.Errorf("results = %d, want 5", got)
	}
	if n := stub.requests.Load(); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
}

func TestSearchView_DropsResultFromSupersededSearch(t *testing.T) {
	v, _ := newStubSearchView(t, 3)
	typeQuery(t, v, "old")
	oldFetch := v.Update(searchDebounceMsg{seq: v.debounceSeq, query: v.searchQuery})
	oldResult := oldFetch().(searchResultMsg)

	typeQuery(t, v, "er") // the user kept typing: a new search supersedes
	newFetch := v.Update(searchDebounceMsg{seq: v.debounceSeq, query: v.searchQuery})

	v.Update(oldResult) // the first search's reply lands late
	if v.pending != 1 || len(v.items) != 0 {
		t.Fatalf("superseded result applied: pending=%d items=%d", v.pending, len(v.items))
	}

	applySearchResult(t, v, newFetch)
	if v.pending != 0 || len(v.items) != 3 {
		t.Errorf("current result not applied: pending=%d items=%d", v.pending, len(v.items))
	}
}

func TestSearchView_DrillDownThenBackRestoresResults(t *testing.T) {
	v, _ := newStubSearchView(t, 4)
	typeQuery(t, v, "a:queen")
	commitSearch(t, v)
	artists := uris(v.items)
	if len(artists) != 4 {
		t.Fatalf("setup: %d artists, want 4", len(artists))
	}

	v.list.Select(2)
	cmd := v.onEnter()
	if cmd == nil {
		t.Fatal("Enter on an artist did not start a drill-down fetch")
	}
	if v.depth != 1 || v.selectedArtist.id != "artist2" {
		t.Fatalf("after drill-down depth=%d artist=%q, want 1/artist2", v.depth, v.selectedArtist.id)
	}
	applySearchResult(t, v, cmd)
	if got := uris(v.items); len(got) != 4 || got[0] != "spotify:album:artist2-album0" {
		t.Fatalf("albums = %v, want artist2's albums", got)
	}
	if bc := v.breadcrumb(); bc != "Home > Search > Artist artist2" {
		t.Errorf("breadcrumb = %q", bc)
	}

	back, handled := v.back()
	if !handled || back == nil {
		t.Fatalf("Back at depth 1: handled=%v cmd=%v, want a refetch", handled, back != nil)
	}
	if v.depth != 0 || v.selectedArtist != (selectedRef{}) {
		t.Errorf("after Back depth=%d artist=%+v, want depth 0 and no selection", v.depth, v.selectedArtist)
	}
	applySearchResult(t, v, back)

	if got := uris(v.items); fmt.Sprint(got) != fmt.Sprint(artists) {
		t.Errorf("results after Back = %v, want %v", got, artists)
	}
	if v.query != "queen" || v.prefix != prefixArtist {
		t.Errorf("search lost across drill-down: query=%q prefix=%v", v.query, v.prefix)
	}
	// Selection: the list keeps its cursor row, so the drilled-into
	// artist is selected again.
	if si, ok := v.list.SelectedItem().(artistItem); !ok || si.id != "artist2" {
		t.Errorf("selected after Back = %#v, want artist2", v.list.SelectedItem())
	}

	if _, handled := v.back(); handled {
		t.Error("Back at depth 0 must be left to the shell (pop the view)")
	}
}

func TestSearchView_FetchMorePaginates(t *testing.T) {
	v, stub := newStubSearchView(t, 25)
	typeQuery(t, v, "song")
	commitSearch(t, v)
	if len(v.items) != 10 || !v.hasMore || v.offset != 10 {
		t.Fatalf("first page: items=%d hasMore=%v offset=%d, want 10/true/10", len(v.items), v.hasMore, v.offset)
	}

	cmd := v.fetchMore()
	if cmd == nil {
		t.Fatal("fetchMore returned nil with more results available")
	}
	if again := v.fetchMore(); again != nil {
		t.Error("fetchMore must not issue a second request while one is pending")
	}
	applySearchResult(t, v, cmd)
	if len(v.items) != 20 || !v.hasMore || v.offset != 20 {
		t.Fatalf("second page: items=%d hasMore=%v offset=%d, want 20/true/20", len(v.items), v.hasMore, v.offset)
	}
	if got := uris(v.items); got[10] != "spotify:track:track10" {
		t.Errorf("second page starts with %q, want track10 (offset not advanced)", got[10])
	}

	applySearchResult(t, v, v.fetchMore())
	if len(v.items) != 25 || v.hasMore {
		t.Errorf("last page: items=%d hasMore=%v, want 25/false", len(v.items), v.hasMore)
	}
	if cmd := v.fetchMore(); cmd != nil {
		t.Error("fetchMore must return nil once the results are exhausted")
	}
	if n := stub.requests.Load(); n != 3 {
		t.Errorf("server saw %d requests, want 3", n)
	}
}

// Moving the cursor near the end of the loaded results pages in the rest.
func TestSearchView_CursorNearEndFetchesMore(t *testing.T) {
	v, _ := newStubSearchView(t, 40)
	typeQuery(t, v, "song")
	v.Update(searchDebounceMsg{seq: v.debounceSeq, query: v.searchQuery})
	// Replace the first page with a bigger one so the threshold is
	// reachable without the first move tripping it.
	v.pending = 0
	v.items = nil
	for i := range 20 {
		v.items = append(v.items, trackItem{uri: fmt.Sprintf("spotify:track:track%d", i)})
	}
	v.offset, v.hasMore = 20, true
	v.rebuildList()

	v.list.Select(13) // 20-14 = 6 rows left after moving down one: no fetch
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.pending != 0 {
		t.Fatalf("fetch started with 6 rows left (cursor %d)", v.list.Index())
	}
	v.Update(tea.KeyMsg{Type: tea.KeyDown}) // cursor 15: searchFetchAhead rows left
	if v.pending != 1 {
		t.Errorf("no fetch with %d rows left (cursor %d)", searchFetchAhead, v.list.Index())
	}
}

// failingPageStub serves a track search of total rows and fails the
// request for one offset until healed.
type failingPageStub struct {
	searchStub
	failOffset int
	healed     atomic.Bool
}

func (s *failingPageStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if off, _ := strconv.Atoi(r.URL.Query().Get("offset")); off == s.failOffset && !s.healed.Load() {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.searchStub.ServeHTTP(w, r)
}

// A later page that fails shows an error row under the loaded results, and
// Enter on it retries that page while the earlier results stay.
func TestSearchView_LaterPageFailureShowsRetryRow(t *testing.T) {
	stub := &failingPageStub{failOffset: 10}
	stub.total = 25
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	client := spotify.New(&http.Client{Transport: &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}})
	v := newSearchView(t.Context(), client, 80, 20, false)
	typeQuery(t, v, "song")
	commitSearch(t, v)

	v.Update(v.fetchMore()())
	rows := v.list.Items()
	if len(rows) != 11 {
		t.Fatalf("%d rows after a failed page 2, want the 10 results and an error row", len(rows))
	}
	if si, ok := rows[10].(statusItem); !ok || !si.isError {
		t.Fatalf("last row = %#v, want the error row", rows[10])
	}
	if !v.hasMore {
		t.Error("hasMore cleared by a failed page; retry would have nothing to resume")
	}
	if cmd := v.fetchMore(); cmd != nil {
		t.Error("fetchMore must hold off after a failure; the error row is the retry")
	}

	stub.healed.Store(true)
	v.list.Select(10)
	cmd := v.onEnter()
	if cmd == nil {
		t.Fatal("Enter on the error row did not retry")
	}
	if rows := v.list.Items(); len(rows) != 11 || len(uris(rows)) != 10 {
		t.Errorf("while retrying: %d rows, %d results, want the 10 results kept plus a loading row", len(rows), len(uris(rows)))
	}
	applySearchResult(t, v, cmd)
	if got := uris(v.items); len(got) != 20 || got[10] != "spotify:track:track10" {
		t.Errorf("after the retry: %d results starting page 2 with %v, want 20 from track10", len(got), got[min(10, len(got)-1)])
	}
}

// Enter drills into an album while the last edit's debounce is pending.
// The tick must not replace the album's tracks with a fresh search.
func TestSearchView_DebounceAfterEnterIsIgnored(t *testing.T) {
	v, _ := newStubSearchView(t, 4)
	typeQuery(t, v, "l:quee")
	commitSearch(t, v)
	typeQuery(t, v, "n") // schedules a debounce for "l:queen"
	pendingSeq, pendingQuery := v.debounceSeq, v.searchQuery

	sc, _ := v.activeSearchInput()
	cmd, _ := handleSearchKey(sc, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || v.depth != 1 {
		t.Fatalf("Enter on an album: depth=%d cmd=%v, want a drill-down", v.depth, cmd != nil)
	}
	album := v.selectedAlbum

	if got := v.Update(searchDebounceMsg{seq: pendingSeq, query: pendingQuery}); got != nil {
		t.Error("the debounce scheduled before Enter started a fetch")
	}
	if v.depth != 1 || v.selectedAlbum != album {
		t.Errorf("drill-down lost: depth=%d album=%+v, want 1/%+v", v.depth, v.selectedAlbum, album)
	}
}

// A fresh page with the cursor on its first row must not request the next
// page on the next unrelated message.
func TestSearchView_FreshPageDoesNotPrefetch(t *testing.T) {
	v, stub := newStubSearchView(t, 25)
	typeQuery(t, v, "song")
	commitSearch(t, v)

	if cmd := v.Update(progressTickMsg{}); v.pending != 0 {
		t.Errorf("a progress tick started a page fetch (cmd=%v)", cmd != nil)
	}
	if n := stub.requests.Load(); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
}
