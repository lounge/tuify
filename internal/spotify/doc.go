// Package spotify wraps the zmb3/spotify Web API client with the
// higher-level operations tuify needs: playlist and library fetches,
// player state polling, playback control, device selection, and
// transfer-on-reconnect behavior.
//
// Client is the operational entry point; construct it with New passing
// the auth-wrapped *http.Client from the auth package. New installs the
// rate-limit gate on that client and builds the zmb3 SDK client on top of
// it, so token refresh and the gate cover SDK and raw HTTP paths alike.
// Client is safe for concurrent use — the underlying zmb3 client and
// http.Client are goroutine-safe, and the atomic DeviceOverridden flag
// coordinates manual-switch awareness between the UI and the librespot
// reconnect handler.
//
// Errors: non-2xx responses surface as *APIError (carrying status and
// truncated body) from every Client method, whether the call went
// through the raw REST path or the SDK. SDK failures are normalized by
// wrapSDKErr; the one exception is an SDK error response whose body is
// empty or not JSON, which the SDK reports as a plain error without a
// status. A cooldown short-circuit surfaces as an *APIError with status
// 429 wrapping *RateLimitedError, so errors.As can recover the deadline.
// Context and network errors are returned unwrapped.
//
// When Spotify rate-limits the client, a shared cooldown is armed so
// subsequent calls short-circuit before hitting the network; callers
// polling on a timer should consult RateLimitWait to extend their
// interval past the deadline. Consecutive 429s escalate
// the cooldown exponentially (up to one hour) so a persistent throttle
// backs off instead of retrying at a fixed interval; the streak resets
// on the first non-429 response.
package spotify
