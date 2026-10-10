// Package auth handles Spotify OAuth2 with PKCE: interactive login via a
// local callback server, token persistence on disk, and proactive token
// refresh to avoid blocking API calls on an expiring token.
//
// Typical flow:
//
//	token, authorizedAt, err := auth.LoadTokenWithAuth()
//	if errors.Is(err, auth.ErrTokenCorrupt) {
//	    token = nil // unreadable token.json: log in again
//	}
//	if token == nil {
//	    token, _ = auth.Login(ctx, authenticator, redirectURL)
//	    _ = auth.SaveFreshToken(token)
//	}
//	httpClient, saveErrCh, revokedCh, cleanup, _ := auth.NewSavingClient(ctx, authenticator, token)
//	defer cleanup()
//
// NewSavingClient returns an *http.Client that refreshes and re-persists
// the token automatically; its cleanup stops the proactive-refresh
// goroutine and returns once it has exited, so cancel ctx first and
// nothing from this package runs after cleanup. The returned client and
// the one refreshes go through honor the HTTP_PROXY, HTTPS_PROXY and
// NO_PROXY environment variables, as the login exchange does. Non-fatal
// auth problems are surfaced on saveErrCh so the UI can warn the user:
// token.json write failures, and a failed refresh at startup (every
// request retries the refresh, so the app keeps running). revokedCh fires
// once if Spotify rejects the refresh token as permanently invalid, so
// the caller can prompt for a fresh login. token.json is written
// atomically (config.WriteFileAtomic).
//
// Errors the package hands on — returned from a request, sent on
// saveErrCh or signalled on revokedCh — are not logged here; the consumer
// logs each once. The proactive-refresh loop is its own consumer: it logs
// a failed refresh and retries after 10s, doubling the wait on each
// further failure up to 60s, and resets to 10s after a success.
//
// Login binds the redirect URL's host and port before it opens the
// browser, so a port that is already taken fails the login at once rather
// than after the user has authorized. Spotify's dashboard accepts a
// loopback IP literal for the redirect (127.0.0.1 or [::1]); a hostname
// such as localhost binds one address family and the browser may pick
// the other. Login's callback server ends the login only for a request
// that carries the state it generated; any other request to the port
// gets a 400 and is ignored, so a stray local request can't abort the
// login. The code-for-token exchange that the callback triggers runs on
// Login's own context with a 30s deadline and a 15s HTTP client timeout,
// not on the callback request's context, so a browser that drops the
// connection as soon as the redirect lands cannot abort it.
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
