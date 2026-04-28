// Package spotify wraps the zmb3/spotify Web API client with the
// higher-level operations tuify needs: playlist and library fetches,
// player state polling, playback control, device selection, and
// transfer-on-reconnect behavior.
//
// Client is the operational entry point; construct it with New passing
// an *sp.Client and *http.Client from the auth package. Both clients
// must share the same auth-wrapped transport so token refresh and the
// rate-limit gate installed by New cover SDK and raw HTTP paths alike.
// Client is safe for concurrent use — the underlying zmb3 client and
// http.Client are goroutine-safe, and the atomic DeviceOverridden flag
// coordinates manual-switch awareness between the UI and the librespot
// reconnect handler.
//
// Errors: non-2xx responses surface as *APIError (carrying status and
// truncated body). When Spotify rate-limits the client, a shared
// cooldown is armed so subsequent calls short-circuit before hitting
// the network; callers polling on a timer should consult RateLimitWait
// to extend their interval past the deadline.
package spotify
