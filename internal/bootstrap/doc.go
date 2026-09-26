// Package bootstrap wires together the application's startup sequence:
// loading or interactively creating config, authenticating against Spotify,
// starting optional librespot playback, and launching the Bubbletea TUI.
//
// Run is the single entry point called from main. The unexported steps it
// composes (setupLog, loadOrSetupConfig, resolveRuntime, authenticate,
// startLibrespot) are individually testable. Run returns ErrInterrupted
// when the process got SIGINT from outside the TUI, so main can exit 130
// without printing an error.
//
// Lifetime: Run owns a root context that is cancelled on return, which
// propagates shutdown to background goroutines started by auth and
// librespot (token refresh, reconnect transfers). SIGHUP (the terminal
// closing) cancels the program the same way SIGINT/SIGTERM do, so the
// terminal is restored and librespot is stopped instead of orphaned.
//
// Logging: setupLog moves the previous run's debug.log to debug.log.1 and
// opens a fresh 0600 debug.log in the config directory.
package bootstrap
