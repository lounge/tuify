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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"net/url"

	"github.com/lounge/tuify/internal/config"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

// savingTokenSource wraps a TokenSource and persists the token to disk
// whenever it is refreshed, so refreshed tokens survive app restarts.
type savingTokenSource struct {
	base oauth2.TokenSource
	last *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		s.last = tok
		if err := SaveToken(tok); err != nil {
			log.Printf("[auth] failed to persist refreshed token: %v", err)
		}
	}
	return tok, nil
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
func NewSavingClient(a *spotifyauth.Authenticator, token *oauth2.Token) *http.Client {
	base := a.Client(context.Background(), token)
	ts := &savingTokenSource{base: base.Transport.(*oauth2.Transport).Source, last: token}
	return &http.Client{
		Transport: &oauth2.Transport{
			Source: ts,
			Base:   http.DefaultTransport,
		},
	}
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

	url := a.AuthURL(state,
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("code_challenge", challenge),
	)
	openBrowser(url)
	fmt.Println("Waiting for authentication...")
	fmt.Printf("If the browser doesn't open, visit:\n  %s\n", url)

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
