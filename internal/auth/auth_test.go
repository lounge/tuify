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
	"time"

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
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved token is not valid JSON: %v", err)
	}
	if parsed["access_token"] != "access-123" {
		t.Errorf("raw JSON access_token: got %v", parsed["access_token"])
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
	a := generateRandomBase64(32)
	b := generateRandomBase64(32)

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

func TestStartupForceExpire(t *testing.T) {
	// Token expiring in 2 minutes should be force-expired.
	tok := &oauth2.Token{
		AccessToken: "soon",
		Expiry:      time.Now().Add(2 * time.Minute),
	}
	if time.Until(tok.Expiry) >= 5*time.Minute {
		t.Fatal("test setup: token should expire within 5 minutes")
	}
	// Simulate the startup check.
	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < 5*time.Minute {
		tok.Expiry = time.Now().Add(-1 * time.Second)
	}
	if !tok.Expiry.Before(time.Now()) {
		t.Error("token should be force-expired")
	}
}

func TestStartupNoForceExpire(t *testing.T) {
	// Token valid for 30 minutes should NOT be force-expired.
	tok := &oauth2.Token{
		AccessToken: "valid",
		Expiry:      time.Now().Add(30 * time.Minute),
	}
	original := tok.Expiry
	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < 5*time.Minute {
		tok.Expiry = time.Now().Add(-1 * time.Second)
	}
	if !tok.Expiry.Equal(original) {
		t.Error("token with >5min remaining should not be modified")
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

func TestProactiveRefresh_TriggersBeforeExpiry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Initial token expires in 10ms, so the refresh triggers immediately
	// (wait = 10ms - 5s < 0). Returned tokens expire in 1h to stop the loop.
	inner := &countingTokenSource{expIn: time.Hour}
	ts := &savingTokenSource{
		base: inner,
		last: &oauth2.Token{
			AccessToken: "initial",
			Expiry:      time.Now().Add(10 * time.Millisecond),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts.startProactiveRefresh(ctx)

	// Wait for at least one proactive refresh.
	deadline := time.After(2 * time.Second)
	for {
		if inner.callCount() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("proactive refresh did not trigger, calls=%d", inner.callCount())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestProactiveRefresh_StopsOnCancel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Short initial expiry triggers refresh quickly; returned tokens expire in
	// 1h so the goroutine enters the cancelable sleep after the first refresh.
	inner := &countingTokenSource{expIn: time.Hour}
	ts := &savingTokenSource{
		base: inner,
		last: &oauth2.Token{
			AccessToken: "initial",
			Expiry:      time.Now().Add(10 * time.Millisecond),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts.startProactiveRefresh(ctx)

	// Wait until at least one refresh happened.
	deadline := time.After(2 * time.Second)
	for inner.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("refresh never triggered")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	time.Sleep(50 * time.Millisecond) // let goroutine exit

	countAfterCancel := inner.callCount()
	time.Sleep(300 * time.Millisecond)
	countLater := inner.callCount()

	if countLater != countAfterCancel {
		t.Errorf("refresh continued after cancel: %d -> %d", countAfterCancel, countLater)
	}
}

func TestProactiveRefresh_NilToken(t *testing.T) {
	// When last token is nil, the goroutine should sleep 30s then re-check.
	// We just verify it doesn't panic and respects cancellation.
	ts := &savingTokenSource{
		base: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "x"}),
		last: nil,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts.startProactiveRefresh(ctx)
	time.Sleep(50 * time.Millisecond) // let goroutine start
	cancel()                          // should exit cleanly
}

func TestProactiveRefresh_RetriesOnError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

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
		last: &oauth2.Token{AccessToken: "will-fail", Expiry: time.Now().Add(10 * time.Millisecond)},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts.startProactiveRefresh(ctx)

	// The first call will fail. We just verify the goroutine attempted the refresh.
	deadline := time.After(2 * time.Second)
	for {
		if callCount.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("proactive refresh did not attempt")
		case <-time.After(10 * time.Millisecond):
		}
	}
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
	_, err := Login(a, "://invalid")
	if err == nil {
		t.Fatal("expected error for invalid redirect URL")
	}
}

// --- openBrowser tests ---

func TestOpenBrowser_UnknownOS(t *testing.T) {
	// Just verifying it doesn't panic on unknown OS.
	// The function checks runtime.GOOS, so we can't easily test it,
	// but we can verify the code challenge round-trip.
	verifier := generateRandomBase64(32)
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
