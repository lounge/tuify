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
// Reconnect: when librespot re-authenticates, reconnectHandler transfers
// playback back to it, and only to it. The device can take a moment to
// appear in Spotify's list, so the handler waits a settle delay and looks
// again a bounded number of times rather than transfer to whichever
// device FindDevice fell back to; a manual device switch (DeviceOverridden)
// stops it at any point. The transfer plays only when the user's play
// intent (spotify.Client.PlayIntent) is not paused, so a pause survives a
// reconnect. When the intent is to play, the handler then reads the player
// state and resumes the device if Spotify left it paused, which happens
// after a broken session. That check runs outside the one-transfer-at-a-
// time guard, so a librespot restart during it still transfers, and a
// newer reconnect ends it.
//
// Icons: useNerdFont resolves the nerd_font setting; when it is omitted,
// the OS font directories are scanned for an installed Nerd Font. This
// only proves the font exists, not that the terminal uses it, so an
// explicit false always wins.
//
// Logging: setupLog moves the previous run's debug.log to debug.log.1 and
// opens a fresh 0600 debug.log in the config directory. When the TUI
// returns, Run logs one "[tuify] exiting:" line naming why (quit, SIGINT,
// SIGHUP, SIGTERM, panic, revoked token or another error) before any
// cleanup runs; the ui package logs the key behind a quit. The UI model
// is wrapped so a panic in Init, Update, View or one of their commands is
// logged with its stack, then re-raised so Bubble Tea still restores the
// terminal: Bubble Tea itself prints a panic only to the terminal.
package bootstrap
