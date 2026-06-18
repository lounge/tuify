package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lounge/tuify/internal/auth"
	"github.com/lounge/tuify/internal/spotify"
	sp "github.com/zmb3/spotify/v2"
)

// refreshTokenLifetime mirrors Spotify's refresh-token expiration policy
// (2026-06-18): refresh tokens become invalid 6 months after the user's
// original authorization, refreshes do not reset the clock.
const refreshTokenLifetime = 6 // months, applied via t.AddDate(0, months, 0)

// reauthWarningWindow is how close to the forced re-auth deadline we
// start logging a heads-up at startup.
const reauthWarningWindow = 30 * 24 * time.Hour

// AuthSession holds the result of authentication.
type AuthSession struct {
	Client    *spotify.Client
	Cleanup   func()
	SaveErrCh <-chan error    // emits token-persistence failures
	RevokedCh <-chan struct{} // fires once if the refresh token is permanently invalid
}

// Authenticate connects to Spotify and returns a ready-to-use session.
// If no saved token exists, it runs the interactive login flow. ctx is the
// parent lifetime — cancelling it aborts login and stops the proactive
// token-refresh goroutine owned by the returned session.
func Authenticate(ctx context.Context, rc RuntimeConfig) (*AuthSession, error) {
	token, authorizedAt, err := auth.LoadTokenWithAuth()
	if err != nil {
		return nil, fmt.Errorf("loading token: %w", err)
	}

	authenticator := auth.NewAuthenticator(rc.ClientID, rc.ResolvedRedirectURL)

	if token == nil {
		fmt.Fprintln(os.Stderr, "No saved session found. Starting login...")
		token, err = auth.Login(ctx, authenticator, rc.ResolvedRedirectURL)
		if err != nil {
			return nil, fmt.Errorf("login failed: %w", err)
		}
		if err := auth.SaveFreshToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save token: %v\n", err)
		}
	} else {
		logReauthWindow(authorizedAt)
	}

	httpClient, saveErrCh, revokedCh, cleanup, err := auth.NewSavingClient(ctx, authenticator, token)
	if err != nil {
		return nil, err
	}

	spClient := sp.New(httpClient)
	client := spotify.New(spClient, httpClient)
	if err := client.FetchUserID(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch user ID: %v\n", err)
	}

	return &AuthSession{
		Client:    client,
		Cleanup:   cleanup,
		SaveErrCh: saveErrCh,
		RevokedCh: revokedCh,
	}, nil
}

// logReauthWindow emits a startup log line when the persisted token is
// approaching Spotify's 6-month refresh-token lifetime. Skips silently if
// authorizedAt is the zero time (older token.json written before the
// timestamp existed — we don't know when authorization happened).
func logReauthWindow(authorizedAt time.Time) {
	if authorizedAt.IsZero() {
		return
	}
	expiresAt := authorizedAt.AddDate(0, refreshTokenLifetime, 0)
	remaining := time.Until(expiresAt)
	switch {
	case remaining <= 0:
		log.Printf("[auth] authorization expired on %s; expect a re-login prompt", expiresAt.Format("2006-01-02"))
	case remaining < reauthWarningWindow:
		log.Printf("[auth] authorization expires on %s (%s remaining); sign in again to avoid an interruption",
			expiresAt.Format("2006-01-02"), remaining.Round(24*time.Hour))
	}
}
