// Package spotify wraps the zmb3/spotify Web API client with the
// higher-level operations tuify needs: playlist and library fetches,
// player state polling, playback control, device selection, and
// transfer-on-reconnect behavior.
//
// Client is the single type exposed; construct it with New passing an
// *sp.Client and *http.Client from the auth package. Client is safe for
// concurrent use — the underlying zmb3 client and http.Client are
// goroutine-safe, and the atomic DeviceOverridden flag coordinates
// manual-switch awareness between the UI and the librespot reconnect
// handler.
package spotify
