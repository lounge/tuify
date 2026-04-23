package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/lounge/tuify/internal/auth"
	"github.com/lounge/tuify/internal/spotify"
	sp "github.com/zmb3/spotify/v2"
)

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
	token, err := auth.LoadToken()
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
		if err := auth.SaveToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save token: %v\n", err)
		}
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
