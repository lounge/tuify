// Package auth handles Spotify OAuth2 with PKCE: interactive login via a
// local callback server, token persistence on disk, and proactive token
// refresh to avoid blocking API calls on an expiring token.
//
// Typical flow:
//
//	token, authorizedAt, _ := auth.LoadTokenWithAuth()
//	if token == nil {
//	    token, _ = auth.Login(ctx, authenticator, redirectURL)
//	    _ = auth.SaveFreshToken(token)
//	}
//	httpClient, saveErrCh, revokedCh, cleanup, _ := auth.NewSavingClient(ctx, authenticator, token)
//	defer cleanup()
//
// NewSavingClient returns an *http.Client that refreshes and re-persists
// the token automatically; its cleanup stops the proactive-refresh
// goroutine. Refresh failures that can't block the caller (disk write
// errors) are surfaced on saveErrCh so the UI can warn the user.
// revokedCh fires once if Spotify rejects the refresh token as
// permanently invalid, so the caller can prompt for a fresh login.
//
// # Refresh-token lifetime
//
// Spotify's 2026-06-18 policy gives refresh tokens a hard 6-month
// lifetime measured from the user's original authorization; access-token
// refreshes do not extend it. The package records the authorization
// moment as `authorized_at` in token.json (via SaveFreshToken) and
// exposes it through LoadTokenWithAuth so callers can warn the user
// before the token expires. When a refresh ultimately fails with
// "invalid_grant", the package signals via revokedCh and deletes the
// stale token file — the next launch will run a fresh login.
package auth
