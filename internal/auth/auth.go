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
		}
	}
	s.mu.Unlock()
	return tok, nil
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
// and persists them to disk on each refresh.
func NewSavingClient(a *spotifyauth.Authenticator, token *oauth2.Token) (*http.Client, error) {
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
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, refreshClient)
	base := a.Client(ctx, token)
	t, ok := base.Transport.(*oauth2.Transport)
	if !ok || t == nil {
		return nil, fmt.Errorf("unexpected transport type from spotify authenticator")
	}
	ts := &savingTokenSource{base: t.Source, last: token}
	// Trigger a refresh now so the token is fresh before any polls start.
	if freshTok, err := ts.Token(); err != nil {
		log.Printf("[auth] startup token refresh failed: %v", err)
	} else {
		log.Printf("[auth] token valid until %v", freshTok.Expiry.Local().Format("15:04:05"))
	}
	ts.startProactiveRefresh(context.Background())
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   transport,
		},
	}, nil
}

func Login(a *spotifyauth.Authenticator, redirectURL string) (*oauth2.Token, error) {
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL: %w", err)
	}
	addr := ":" + parsed.Port()

	verifier := generateRandomBase64(32)
	challenge := generateCodeChallenge(verifier)
	state := generateRandomBase64(16)

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
	}
}

func SaveToken(token *oauth2.Token) error {
	dir := config.Dir()
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
	data, err := os.ReadFile(filepath.Join(config.Dir(), "token.json"))
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

func generateRandomBase64(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
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
