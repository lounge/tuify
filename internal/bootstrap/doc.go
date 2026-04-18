// Package bootstrap wires together the application's startup sequence:
// loading or interactively creating config, authenticating against Spotify,
// starting optional librespot playback, and launching the Bubbletea TUI.
//
// Run is the single entry point called from main. The other exported
// functions (SetupLog, LoadOrSetupConfig, ResolveRuntime, Authenticate,
// StartLibrespot) are composed by Run but are also individually testable.
//
// Lifetime: Run owns a root context that is cancelled on return, which
// propagates shutdown to background goroutines started by auth and
// librespot (token refresh, reconnect transfers).
package bootstrap
