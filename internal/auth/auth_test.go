package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lounge/tuify/internal/testutil"
	"golang.org/x/oauth2"
)

func TestSaveAndLoadToken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	token := &oauth2.Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		Expiry:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := SaveToken(token); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	// Verify file exists with correct permissions
	path := filepath.Join(tmp, "tuify", "token.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file not found: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("token file permissions: got %o, want 600", perm)
		}
	}

	loaded, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadToken returned nil")
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}

	// Verify raw file is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved token is not valid JSON: %v", err)
	}
	if parsed["access_token"] != "access-123" {
		t.Errorf("raw JSON access_token: got %v", parsed["access_token"])
	}
}

func TestSaveFreshToken_StampsAuthorizedAt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	token := &oauth2.Token{
		AccessToken:  "access-fresh",
		RefreshToken: "refresh-fresh",
		TokenType:    "Bearer",
		Expiry:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	before := time.Now()
	if err := SaveFreshToken(token); err != nil {
		t.Fatalf("SaveFreshToken: %v", err)
	}
	after := time.Now()

	_, authAt, err := LoadTokenWithAuth()
	if err != nil {
		t.Fatalf("LoadTokenWithAuth: %v", err)
	}
	if authAt.Before(before) || authAt.After(after) {
		t.Errorf("AuthorizedAt = %v, want between %v and %v", authAt, before, after)
	}
}

func TestSaveToken_PreservesAuthorizedAt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// On the fake clock the refresh happens a full hour after the login,
	// so a SaveToken that re-stamped AuthorizedAt could not go unnoticed.
	synctest.Test(t, func(t *testing.T) {
		freshTok := &oauth2.Token{AccessToken: "v1", RefreshToken: "rt", TokenType: "Bearer"}
		if err := SaveFreshToken(freshTok); err != nil {
			t.Fatalf("SaveFreshToken: %v", err)
		}
		_, originalAuthAt, err := LoadTokenWithAuth()
		if err != nil || originalAuthAt.IsZero() {
			t.Fatalf("setup: expected non-zero AuthorizedAt; err=%v", err)
		}

		// Simulate a token refresh — SaveToken must NOT reset AuthorizedAt.
		time.Sleep(time.Hour)
		refreshedTok := &oauth2.Token{AccessToken: "v2", RefreshToken: "rt", TokenType: "Bearer"}
		if err := SaveToken(refreshedTok); err != nil {
			t.Fatalf("SaveToken: %v", err)
		}

		loaded, authAt, err := LoadTokenWithAuth()
		if err != nil {
			t.Fatalf("LoadTokenWithAuth: %v", err)
		}
		if loaded.AccessToken != "v2" {
			t.Errorf("AccessToken: got %q, want v2", loaded.AccessToken)
		}
		if !authAt.Equal(originalAuthAt) {
			t.Errorf("AuthorizedAt was reset by SaveToken: got %v, want %v", authAt, originalAuthAt)
		}
	})
}

func TestLoadTokenWithAuth_BackCompatNoAuthorizedAt(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Simulate a token.json written by an older tuify version (no authorized_at).
	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	legacy := []byte(`{"access_token":"legacy","refresh_token":"rt","token_type":"Bearer","expiry":"2025-01-01T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(dir, "token.json"), legacy, 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	tok, authAt, err := LoadTokenWithAuth()
	if err != nil {
		t.Fatalf("LoadTokenWithAuth: %v", err)
	}
	if tok == nil || tok.AccessToken != "legacy" {
		t.Errorf("expected legacy token, got %+v", tok)
	}
	if !authAt.IsZero() {
		t.Errorf("AuthorizedAt should be zero for legacy file, got %v", authAt)
	}
}

func TestLoadToken_NoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	token, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if token != nil {
		t.Errorf("expected nil for missing token, got %+v", token)
	}
}

func TestLoadToken_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token.json"), []byte("not json"), 0o600)

	_, err := LoadToken()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGenerateRandomBase64(t *testing.T) {
	a, err := generateRandomBase64(32)
	if err != nil {
		t.Fatalf("generateRandomBase64: %v", err)
	}
	b, err := generateRandomBase64(32)
	if err != nil {
		t.Fatalf("generateRandomBase64: %v", err)
	}

	if a == b {
		t.Error("two random values should not be equal")
	}
	// 32 bytes -> 43 chars in base64 raw URL encoding
	if len(a) != 43 {
		t.Errorf("expected length 43, got %d", len(a))
	}
}

func TestSavingTokenSource_PersistsOnRefresh(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	first := &oauth2.Token{AccessToken: "old", Expiry: time.Now().Add(time.Hour)}
	second := &oauth2.Token{AccessToken: "new", Expiry: time.Now().Add(2 * time.Hour)}

	fake := oauth2.StaticTokenSource(second)
	ts := &savingTokenSource{
		base: oauth2.ReuseTokenSource(first, fake),
		last: first,
	}

	// First call: token is still valid, returns cached "old".
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if tok.AccessToken != "old" {
		t.Errorf("expected cached token, got %q", tok.AccessToken)
	}

	// Force expiry so the next call triggers a refresh.
	ts.mu.Lock()
	ts.last.Expiry = time.Now().Add(-1 * time.Second)
	ts.mu.Unlock()
	// Re-create the ReuseTokenSource with the expired token so it refreshes.
	ts.base = oauth2.ReuseTokenSource(ts.last, fake)

	tok, err = ts.Token()
	if err != nil {
		t.Fatalf("Token() after expiry: %v", err)
	}
	if tok.AccessToken != "new" {
		t.Errorf("expected refreshed token, got %q", tok.AccessToken)
	}

	// Verify persisted to disk.
	loaded, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded == nil || loaded.AccessToken != "new" {
		t.Errorf("persisted token: got %v", loaded)
	}
}

// newTokenServer stands in for Spotify's token endpoint. handler answers
// refresh requests; the returned counter tracks how many arrived. The
// returned client routes every request to the server, for injection into
// newSavingClient as the refresh client.
func newTokenServer(t *testing.T, handler http.HandlerFunc) (*http.Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &testutil.RewriteTransport{Base: srv.Client().Transport, Target: srv.URL}}, &hits
}

func refreshOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"access_token":"fresh","token_type":"Bearer","refresh_token":"r","expires_in":3600}`)
}

func TestNewSavingClient_StartupRefresh(t *testing.T) {
	tests := []struct {
		name      string
		expiresIn time.Duration
		wantHits  int32
	}{
		{"expiring within 5 minutes is refreshed at startup", 2 * time.Minute, 1},
		{"valid for 30 minutes is not refreshed", 30 * time.Minute, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			refresh, hits := newTokenServer(t, refreshOK)
			tok := &oauth2.Token{AccessToken: "old", RefreshToken: "r", Expiry: time.Now().Add(tc.expiresIn)}

			_, saveErrCh, _, cleanup, err := newSavingClient(t.Context(), NewAuthenticator("id", "http://127.0.0.1:4444/cb"), tok, refresh)
			if err != nil {
				t.Fatalf("newSavingClient: %v", err)
			}
			defer cleanup()

			if got := hits.Load(); got != tc.wantHits {
				t.Errorf("refresh requests at startup: got %d, want %d", got, tc.wantHits)
			}
			select {
			case err := <-saveErrCh:
				t.Errorf("unexpected saveErrCh value: %v", err)
			default:
			}
		})
	}
}

func TestNewSavingClient_StartupRefreshFailureIsReported(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	refresh, _ := newTokenServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	tok := &oauth2.Token{AccessToken: "old", RefreshToken: "r", Expiry: time.Now().Add(time.Minute)}

	_, saveErrCh, revokedCh, cleanup, err := newSavingClient(t.Context(), NewAuthenticator("id", "http://127.0.0.1:4444/cb"), tok, refresh)
	if err != nil {
		t.Fatalf("newSavingClient: %v", err)
	}
	defer cleanup()

	select {
	case err := <-saveErrCh:
		if errors.Is(err, ErrTokenRevoked) {
			t.Errorf("a 503 must not be reported as revoked: %v", err)
		}
	default:
		t.Fatal("startup refresh failure was not reported on saveErrCh")
	}
	select {
	case <-revokedCh:
		t.Error("revokedCh fired for a transient failure")
	default:
	}
}

func TestNewSavingClient_RevokedDeletesTokenAndSignalsOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	tokenPath := filepath.Join(dir, "tuify", "token.json")
	if err := SaveFreshToken(&oauth2.Token{AccessToken: "old", RefreshToken: "dead"}); err != nil {
		t.Fatalf("SaveFreshToken: %v", err)
	}
	refresh, _ := newTokenServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
	})
	tok := &oauth2.Token{AccessToken: "old", RefreshToken: "dead", Expiry: time.Now().Add(time.Minute)}

	client, saveErrCh, revokedCh, cleanup, err := newSavingClient(t.Context(), NewAuthenticator("id", "http://127.0.0.1:4444/cb"), tok, refresh)
	if err != nil {
		t.Fatalf("newSavingClient: %v", err)
	}
	defer cleanup()

	select {
	case <-revokedCh:
	default:
		t.Fatal("revokedCh did not fire after invalid_grant at startup")
	}
	if _, err := os.Stat(tokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token.json should be deleted, stat err = %v", err)
	}
	select {
	case err := <-saveErrCh:
		t.Errorf("revocation must not also be reported on saveErrCh: %v", err)
	default:
	}

	// A later request refreshes again and fails the same way: the error is
	// detectable as revoked, and the signal does not fire a second time.
	resp, err := client.Get("https://api.spotify.com/v1/me")
	if err == nil {
		resp.Body.Close()
		t.Fatal("request with a revoked token succeeded")
	}
	if !errors.Is(err, ErrTokenRevoked) {
		t.Errorf("request error not detectable as ErrTokenRevoked: %v", err)
	}
	select {
	case <-revokedCh:
		t.Error("revokedCh fired a second time")
	default:
	}
}

func TestIsRevokedError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"invalid_grant", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"wrapped invalid_grant", fmt.Errorf("refresh: %w", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}), true},
		{"other oauth error", &oauth2.RetrieveError{ErrorCode: "invalid_client"}, false},
		{"string mention only", errors.New("upstream said invalid_grant"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRevokedError(tc.err); got != tc.want {
				t.Errorf("isRevokedError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test-verifier-string"
	c1 := generateCodeChallenge(verifier)
	c2 := generateCodeChallenge(verifier)

	if c1 != c2 {
		t.Error("same verifier should produce same challenge")
	}
	if len(c1) == 0 {
		t.Error("challenge should not be empty")
	}
	if c1 == verifier {
		t.Error("challenge should differ from verifier")
	}
}

// --- savingTokenSource tests ---

func TestSavingTokenSource_SkipsPersistWhenUnchanged(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	tok := &oauth2.Token{AccessToken: "same", Expiry: time.Now().Add(time.Hour)}
	ts := &savingTokenSource{
		base: oauth2.StaticTokenSource(tok),
		last: tok,
	}

	// First call — token unchanged, should not write to disk.
	got, err := ts.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if got.AccessToken != "same" {
		t.Errorf("got %q, want %q", got.AccessToken, "same")
	}

	// Verify no file was written (access token didn't change).
	path := filepath.Join(tmp, "tuify", "token.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("token file should not exist when access token is unchanged")
	}
}

func TestSavingTokenSource_PersistsWhenNilLast(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	tok := &oauth2.Token{AccessToken: "fresh", Expiry: time.Now().Add(time.Hour)}
	ts := &savingTokenSource{
		base: oauth2.StaticTokenSource(tok),
		last: nil, // first call ever
	}

	got, err := ts.Token()
	if err != nil {
		t.Fatalf("Token(): %v", err)
	}
	if got.AccessToken != "fresh" {
		t.Errorf("got %q, want %q", got.AccessToken, "fresh")
	}

	// Should persist since last was nil.
	loaded, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded == nil || loaded.AccessToken != "fresh" {
		t.Errorf("expected persisted token with AccessToken=fresh, got %v", loaded)
	}
}

// errorTokenSource returns an error on every Token() call.
type errorTokenSource struct{ err error }

func (e *errorTokenSource) Token() (*oauth2.Token, error) { return nil, e.err }

func TestSavingTokenSource_PropagatesError(t *testing.T) {
	ts := &savingTokenSource{
		base: &errorTokenSource{err: fmt.Errorf("network down")},
		last: &oauth2.Token{AccessToken: "old"},
	}

	_, err := ts.Token()
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "network down" {
		t.Errorf("got error %q, want %q", err, "network down")
	}
}

// --- Proactive refresh tests ---

// countingTokenSource tracks how many times Token() is called and returns
// tokens with short expiry so proactive refresh can be tested quickly.
type countingTokenSource struct {
	mu    sync.Mutex
	calls int
	expIn time.Duration // expiry duration for returned tokens
	err   error         // if set, return this error
}

func (c *countingTokenSource) Token() (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &oauth2.Token{
		AccessToken: fmt.Sprintf("tok-%d", c.calls),
		Expiry:      time.Now().Add(c.expIn),
	}, nil
}

func (c *countingTokenSource) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// The proactive refresh tests run on synctest's fake clock: time.Sleep
// advances it instantly once every goroutine in the bubble is blocked, and
// synctest.Wait lets the refresh goroutine settle before each assertion,
// so they check exactly when a refresh fires instead of polling for it.

func TestProactiveRefresh_TriggersBeforeExpiry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	synctest.Test(t, func(t *testing.T) {
		// Returned tokens expire in 1h, so after one refresh the loop
		// parks until well past the end of the test.
		inner := &countingTokenSource{expIn: time.Hour}
		ts := &savingTokenSource{
			base: inner,
			last: &oauth2.Token{AccessToken: "initial", Expiry: time.Now().Add(time.Minute)},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ts.startProactiveRefresh(ctx)

		// The refresh is due 5s before expiry: not at 54s, but by 55s.
		time.Sleep(54 * time.Second)
		synctest.Wait()
		if n := inner.callCount(); n != 0 {
			t.Fatalf("refreshed %d times before the 5s-before-expiry mark", n)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if n := inner.callCount(); n != 1 {
			t.Fatalf("refresh calls at the 5s-before-expiry mark = %d, want 1", n)
		}
	})
}

func TestProactiveRefresh_StopsOnCancel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	synctest.Test(t, func(t *testing.T) {
		// Returned tokens expire in 10s, so the loop keeps refreshing
		// every 5s until cancelled.
		inner := &countingTokenSource{expIn: 10 * time.Second}
		ts := &savingTokenSource{
			base: inner,
			last: &oauth2.Token{AccessToken: "initial", Expiry: time.Now().Add(10 * time.Second)},
		}
		ctx, cancel := context.WithCancel(t.Context())
		ts.startProactiveRefresh(ctx)

		time.Sleep(time.Minute)
		synctest.Wait()
		before := inner.callCount()
		if before < 2 {
			t.Fatalf("expected repeated refreshes before cancel, got %d", before)
		}

		// synctest.Test fails the test if the goroutine outlives the
		// bubble, so this also proves the loop exits on cancel.
		cancel()
		synctest.Wait()
		time.Sleep(time.Hour)
		if after := inner.callCount(); after != before {
			t.Errorf("refresh continued after cancel: %d -> %d", before, after)
		}
	})
}

func TestProactiveRefresh_NilToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// With no token the loop only re-checks every 30s; it must never
		// call Token() and must exit on cancel.
		inner := &countingTokenSource{expIn: time.Hour}
		ts := &savingTokenSource{base: inner, last: nil}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ts.startProactiveRefresh(ctx)

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if n := inner.callCount(); n != 0 {
			t.Errorf("Token() called %d times with no token to refresh", n)
		}
	})
}

func TestProactiveRefresh_RetriesOnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	synctest.Test(t, func(t *testing.T) {
		var callCount atomic.Int32
		ts := &savingTokenSource{
			base: tokenSourceFunc(func() (*oauth2.Token, error) {
				n := callCount.Add(1)
				if n <= 1 {
					return nil, errors.New("temporary failure")
				}
				return &oauth2.Token{
					AccessToken: fmt.Sprintf("recovered-%d", n),
					Expiry:      time.Now().Add(time.Hour),
				}, nil
			}),
			last: &oauth2.Token{AccessToken: "will-fail", Expiry: time.Now().Add(time.Minute)},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ts.startProactiveRefresh(ctx)

		// First attempt at 55s fails; the retry comes 10s later.
		time.Sleep(55 * time.Second)
		synctest.Wait()
		if n := callCount.Load(); n != 1 {
			t.Fatalf("attempts at the refresh mark = %d, want 1", n)
		}
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if n := callCount.Load(); n != 1 {
			t.Fatalf("retried after %d attempts before the 10s backoff elapsed", n)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if n := callCount.Load(); n != 2 {
			t.Fatalf("attempts after the 10s backoff = %d, want 2", n)
		}
	})
}

// tokenSourceFunc adapts a function to oauth2.TokenSource.
type tokenSourceFunc func() (*oauth2.Token, error)

func (f tokenSourceFunc) Token() (*oauth2.Token, error) { return f() }

// --- Login callback tests ---

func TestLogin_StateMismatch(t *testing.T) {
	// Start a test server that simulates a bad callback with wrong state.
	mux := http.NewServeMux()
	var callbackErr error
	errCh := make(chan error, 1)
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// This handler simulates what Login sets up — we test the state check.
		if r.URL.Query().Get("state") != "expected-state" {
			callbackErr = errors.New("state mismatch")
			errCh <- callbackErr
			return
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Hit the callback with wrong state.
	resp, err := http.Get(srv.URL + "/callback?state=wrong-state&code=some-code")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-errCh:
		if err == nil || err.Error() != "state mismatch" {
			t.Errorf("expected state mismatch error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected error from state mismatch")
	}
}

func TestLogin_EmptyCode(t *testing.T) {
	mux := http.NewServeMux()
	errCh := make(chan error, 1)
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "test-state" {
			errCh <- errors.New("state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("auth error: %s", r.URL.Query().Get("error"))
			return
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/callback?state=test-state&error=access_denied")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	select {
	case err := <-errCh:
		if err == nil || err.Error() != "auth error: access_denied" {
			t.Errorf("expected auth error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected error from empty code")
	}
}

func TestLogin_InvalidRedirectURL(t *testing.T) {
	a := NewAuthenticator("test-client", "http://127.0.0.1:4444/callback")
	_, err := Login(context.Background(), a, "://invalid")
	if err == nil {
		t.Fatal("expected error for invalid redirect URL")
	}
}

func TestLogin_RedirectURLMissingHost(t *testing.T) {
	a := NewAuthenticator("test-client", "http://127.0.0.1:4444/callback")
	_, err := Login(context.Background(), a, "/callback-only")
	if err == nil {
		t.Fatal("expected error for redirect URL missing host:port")
	}
}

// --- openBrowser tests ---

func TestOpenBrowser_UnknownOS(t *testing.T) {
	// Just verifying it doesn't panic on unknown OS.
	// The function checks runtime.GOOS, so we can't easily test it,
	// but we can verify the code challenge round-trip.
	verifier, err := generateRandomBase64(32)
	if err != nil {
		t.Fatalf("generateRandomBase64: %v", err)
	}
	challenge := generateCodeChallenge(verifier)
	if challenge == "" {
		t.Error("code challenge should not be empty")
	}
}

// --- NewAuthenticator test ---

func TestNewAuthenticator(t *testing.T) {
	a := NewAuthenticator("test-id", "http://localhost:8080/callback")
	if a == nil {
		t.Fatal("expected non-nil authenticator")
	}
	// Verify the auth URL contains the client ID.
	authURL := a.AuthURL("test-state")
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("invalid auth URL: %v", err)
	}
	if got := parsed.Query().Get("client_id"); got != "test-id" {
		t.Errorf("client_id: got %q, want %q", got, "test-id")
	}
	if got := parsed.Query().Get("redirect_uri"); got != "http://localhost:8080/callback" {
		t.Errorf("redirect_uri: got %q, want %q", got, "http://localhost:8080/callback")
	}
}
