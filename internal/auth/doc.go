// Package auth handles Spotify OAuth2 with PKCE: interactive login via a
// local callback server, token persistence on disk, and proactive token
// refresh to avoid blocking API calls on an expiring token.
//
// Typical flow:
//
//	token, _ := auth.LoadToken()
//	if token == nil {
//	    token, _ = auth.Login(ctx, authenticator, redirectURL)
//	    _ = auth.SaveToken(token)
//	}
//	httpClient, saveErrCh, cleanup, _ := auth.NewSavingClient(ctx, authenticator, token)
//	defer cleanup()
//
// NewSavingClient returns an *http.Client that refreshes and re-persists
// the token automatically; its cleanup stops the proactive-refresh
// goroutine. Refresh failures that can't block the caller (disk write
// errors) are surfaced on saveErrCh so the UI can warn the user.
package auth
