package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/lounge/tuify/internal/config"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

// savingTokenSource wraps a TokenSource and persists the token to disk
// whenever it is refreshed, so refreshed tokens survive app restarts.
type savingTokenSource struct {
	mu     sync.Mutex
	base   oauth2.TokenSource
	last   *oauth2.Token
	cancel context.CancelFunc

	// saveErrCh receives persistence failures. Buffered and lossy on full so
	// we never block a refresh. Consumers (the UI) render these as visible
	// warnings; without this signal a refresh failure is silent and the user
	// only notices on the next app restart when they're forced to re-login.
	saveErrCh chan error
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	start := time.Now()
	tok, err := s.base.Token()
	if elapsed := time.Since(start); elapsed > time.Second {
		log.Printf("[auth] Token() took %v (err=%v)", elapsed.Round(time.Millisecond), err)
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		s.last = tok
		if err := SaveToken(tok); err != nil {
			log.Printf("[auth] failed to persist refreshed token: %v", err)
			s.notifySaveErr(err)
		}
	}
	s.mu.Unlock()
	return tok, nil
}

// notifySaveErr pushes a save failure onto the channel without blocking.
func (s *savingTokenSource) notifySaveErr(err error) {
	if s.saveErrCh == nil {
		return
	}
	select {
	case s.saveErrCh <- err:
	default:
	}
}

// startProactiveRefresh runs a background goroutine that refreshes the token
// before it expires, preventing request-time refresh blocking. The goroutine
// exits when the context is cancelled. Call the returned cancel function (also
// stored as s.cancel) to stop the goroutine.
func (s *savingTokenSource) startProactiveRefresh(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go func() {
		for {
			s.mu.Lock()
			tok := s.last
			s.mu.Unlock()

			if tok == nil || tok.Expiry.IsZero() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(30 * time.Second):
					continue
				}
			}

			// Sleep until 5s before the token expires. The oauth2
			// ReuseTokenSource refreshes with a 10s margin, so at 5s
			// before expiry a Token() call triggers a real refresh.
			wait := time.Until(tok.Expiry.Add(-5 * time.Second))
			if wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}

			log.Printf("[auth] proactive token refresh")
			newTok, err := s.Token()
			if err != nil {
				log.Printf("[auth] proactive token refresh failed: %v", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
				}
			} else {
				log.Printf("[auth] token refreshed, valid until %v", newTok.Expiry.Local().Format("15:04:05"))
			}
		}
	}()
}

func NewAuthenticator(clientID, redirectURL string) *spotifyauth.Authenticator {
	return spotifyauth.New(
		spotifyauth.WithClientID(clientID),
		spotifyauth.WithRedirectURL(redirectURL),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadPlaybackState,
			spotifyauth.ScopeUserModifyPlaybackState,
			spotifyauth.ScopePlaylistReadPrivate,
			spotifyauth.ScopePlaylistReadCollaborative,
			spotifyauth.ScopeUserLibraryRead,
		),
	)
}

// NewSavingClient creates an HTTP client that auto-refreshes OAuth tokens
// and persists them to disk on each refresh. The returned cleanup function
// stops the proactive-refresh goroutine; callers must invoke it on shutdown.
// saveErrCh emits persistence failures so the caller can surface them to
// the user (e.g. a status banner); it is buffered and lossy on full.
// ctx is the parent lifetime: when it is cancelled, the proactive-refresh
// goroutine exits and in-flight oauth2 refresh requests are cancelled too.
func NewSavingClient(ctx context.Context, a *spotifyauth.Authenticator, token *oauth2.Token) (*http.Client, <-chan error, func(), error) {
	// Provide a timeout-configured client for oauth2 token refresh requests.
	// Without this, token refreshes use http.DefaultClient (no timeouts) and
	// a hanging refresh blocks ALL API calls behind the oauth2 mutex.
	refreshClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}
	// If the token expires within 5 minutes, force it to appear expired so
	// the oauth2 library refreshes immediately during startup (no concurrent
	// requests yet). This prevents the first in-flight refresh from blocking
	// all API calls behind the oauth2 mutex.
	if !token.Expiry.IsZero() && time.Until(token.Expiry) < 5*time.Minute {
		log.Printf("[auth] token expires in %v, refreshing at startup", time.Until(token.Expiry).Round(time.Second))
		token.Expiry = time.Now().Add(-1 * time.Second)
	}
	oauthCtx := context.WithValue(ctx, oauth2.HTTPClient, refreshClient)
	base := a.Client(oauthCtx, token)
	t, ok := base.Transport.(*oauth2.Transport)
	if !ok || t == nil {
		return nil, nil, nil, fmt.Errorf("unexpected transport type from spotify authenticator")
	}
	saveErrCh := make(chan error, 4)
	ts := &savingTokenSource{base: t.Source, last: token, saveErrCh: saveErrCh}
	// Trigger a refresh now so the token is fresh before any polls start.
	if freshTok, err := ts.Token(); err != nil {
		log.Printf("[auth] startup token refresh failed: %v", err)
	} else {
		log.Printf("[auth] token valid until %v", freshTok.Expiry.Local().Format("15:04:05"))
	}
	ts.startProactiveRefresh(ctx)
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableKeepAlives:     true,
	}
	cleanup := func() {
		if ts.cancel != nil {
			ts.cancel()
		}
	}
	return &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   transport,
		},
	}, saveErrCh, cleanup, nil
}

// Login runs the interactive PKCE flow: spins up a local callback server,
// opens the browser, and blocks until the user completes auth. Cancel ctx
// to abort a login that is stuck waiting for the browser callback.
func Login(ctx context.Context, a *spotifyauth.Authenticator, redirectURL string) (*oauth2.Token, error) {
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL: %w", err)
	}
	addr := ":" + parsed.Port()

	verifier, err := generateRandomBase64(32)
	if err != nil {
		return nil, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	challenge := generateCodeChallenge(verifier)
	state, err := generateRandomBase64(16)
	if err != nil {
		return nil, fmt.Errorf("generate OAuth state: %w", err)
	}

	tokenCh := make(chan *oauth2.Token, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- errors.New("state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("auth error: %s", r.URL.Query().Get("error"))
			return
		}
		token, err := a.Exchange(
			r.Context(),
			code,
			oauth2.SetAuthURLParam("code_verifier", verifier),
		)
		if err != nil {
			errCh <- err
			return
		}
		fmt.Fprint(w, "<html><body><h1>Success!</h1><p>You can close this window.</p></body></html>")
		tokenCh <- token
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start auth server: %w", err)
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	authURL := a.AuthURL(state,
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", challenge),
	)
	openBrowser(authURL)
	fmt.Println("Waiting for authentication...")
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n", authURL)

	select {
	case token := <-tokenCh:
		return token, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func SaveToken(token *oauth2.Token) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "token.json"), data, 0o600)
}

func LoadToken() (*oauth2.Token, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "token.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func generateRandomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[auth] failed to open browser: %v", err)
		return
	}
	go cmd.Wait()
}
