// Package spotify wraps the zmb3/spotify Web API client with the
// higher-level operations tuify needs: playlist and library fetches,
// player state polling, playback control, device selection, and
// transfer-on-reconnect behavior.
//
// Client is the operational entry point; construct it with New passing
// the auth-wrapped *http.Client from the auth package. New installs the
// rate-limit gate on that client and builds the zmb3 SDK client on top of
// it, so token refresh and the gate cover SDK and raw HTTP paths alike.
// Options: WithPreferredDevice names tuify's own Connect device, which
// FindDevice prefers; PreferredDevice reads it back and never changes after
// New.
// Client is safe for concurrent use — the underlying zmb3 client and
// http.Client are goroutine-safe, and the atomic DeviceOverridden flag
// coordinates manual-switch awareness between the UI and the librespot
// reconnect handler.
//
// Errors: non-2xx responses surface as *APIError (carrying status and
// truncated body) from every Client method, whether the call went
// through the raw REST path or the SDK. SDK failures are normalized by
// wrapSDKErr. An error response whose body is not Spotify's error JSON
// (an HTML page from a gateway, an empty body) is given that shape by the
// transport before either path reads it, so the status survives and the
// page itself never becomes an error message; the dropped body is logged
// once, truncated. A cooldown short-circuit surfaces as an *APIError with
// status 429 wrapping *RateLimitedError, so errors.As can recover the
// deadline. Context and network errors are returned unwrapped.
//
// When Spotify rate-limits the client, a shared cooldown is armed so
// subsequent calls short-circuit before hitting the network; callers
// polling on a timer should consult RateLimitWait to extend their
// interval past the deadline. A 429 whose Retry-After is missing, zero or
// negative arms the cooldown; only a positive value of a few seconds is
// retried inline, and a request whose inline retries are all throttled
// arms it too, as one throttle. Consecutive 429s escalate
// the cooldown exponentially (up to one hour) so a persistent throttle
// backs off instead of retrying at a fixed interval; the streak resets
// on the first non-429 response. 429s for requests that were in flight
// together count as one throttle.
//
// Paging: every paged method reports whether another page follows. An
// empty page never does, whatever Spotify's total says, so a caller that
// loops on it always terminates; search paging also stops at the
// endpoint's 1000-result cap, past which Spotify answers 400. Methods that
// drop entries (GetPlaylists, GetPlaylistTracks) also return the raw page
// size, which is what a caller must advance its offset by.
//
// Devices: FindDevice never returns a device Spotify lists without an ID,
// and its last-resort pick skips restricted devices, which accept no Web
// API commands.
//
// Text: every name the package returns (tracks, artists, albums,
// playlists and their owners, shows, episodes, devices), along with
// device types, release dates and artist genres, has been passed through
// termsafe.Clean, so callers can render it without escaping.
package spotify
