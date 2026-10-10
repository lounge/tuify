package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/theme"
	"github.com/lounge/tuify/internal/ui"
	zone "github.com/lrstanley/bubblezone"
)

// ErrInterrupted is returned by Run when the process got SIGINT from
// outside the TUI. main exits with the conventional status 130 for it.
var ErrInterrupted = errors.New("interrupted")

// Run is the main application entry point. It loads config, authenticates,
// starts services, and runs the TUI. Returns an error on startup or runtime
// failure.
//
// The supporting pieces live in sibling files: setup.go (log/config/runtime
// resolve), auth_session.go (Spotify auth), librespot.go (optional local
// playback subprocess + audio pipe).
func Run() error {
	closeLog := setupLog()
	defer closeLog()

	// Root context for the whole app run. Cancelled as soon as the UI
	// exits (and on any early return) so every background goroutine
	// (token refresh, device polling, librespot reconnect ops) unwinds
	// before the deferred cleanups run instead of racing them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadOrSetupConfig(nil, nil)
	if err != nil {
		return err
	}
	// A loaded config was validated before anything wrote to it; this
	// also covers the one first-time setup just created.
	if err := validateConfig(cfg); err != nil {
		return err
	}

	// Force or autodetect terminal background mode before any rendering.
	// AdaptiveColor lookups read this at render time, so it has to land
	// before the first View() call.
	switch cfg.Appearance {
	case "dark":
		lipgloss.SetHasDarkBackground(true)
	case "light":
		lipgloss.SetHasDarkBackground(false)
	default:
		// Autodetect. lipgloss answers the first HasDarkBackground call by
		// asking the terminal for its background colour (termenv OSC 11,
		// up to a 5s wait for the reply) and caches the result. Left to
		// chance, that first call lands inside View — the progress bar
		// gradient, album art and lyrics all consult it — and stalls the
		// first frame with the alt screen already up. Asking here, with
		// the terminal still in its normal state, moves the wait before
		// the program starts and makes every render-time call a cache hit.
		lipgloss.HasDarkBackground()
	}

	// Apply theme overrides before any UI rendering. ui.RebuildStyles
	// reconstructs every package-level lipgloss.Style with the new palette
	// (lipgloss captures colors by value at style construction time).
	theme.Apply(cfg.Theme)
	ui.RebuildStyles()

	rc := resolveRuntime(cfg)

	session, err := authenticate(ctx, rc)
	if err != nil {
		return err
	}
	if session.Cleanup != nil {
		defer session.Cleanup()
	}

	var opts []ui.ModelOption
	if cfg.VimMode {
		opts = append(opts, ui.WithVimMode())
	}
	if useNerdFont(cfg.NerdFont) {
		opts = append(opts, ui.WithNerdFont())
	}
	if session.SaveErrCh != nil {
		opts = append(opts, ui.WithTokenSaveErrors(session.SaveErrCh))
	}

	// Fan the auth-session revocation signal into both the UI (to trigger
	// a clean shutdown) and a local atomic flag (so after p.Run returns
	// we know to print the re-login message instead of the raw tea error).
	var tokenRevoked atomic.Bool
	if session.RevokedCh != nil {
		uiCh := make(chan struct{}, 1)
		go func() {
			select {
			case _, ok := <-session.RevokedCh:
				if !ok {
					return
				}
			case <-ctx.Done():
				return
			}
			tokenRevoked.Store(true)
			uiCh <- struct{}{}
		}()
		opts = append(opts, ui.WithTokenRevoked(uiCh))
	}

	svc, err := startLibrespot(ctx, rc, session.Client)
	if err != nil {
		return err
	}
	if svc != nil {
		defer svc.Cleanup()
		opts = append(opts, svc.Options...)
	}

	// Initialize the bubblezone global manager so the UI can mark
	// clickable regions in rendered output and resolve mouse clicks to
	// specific list items. Must be called before any zone.Mark/Scan use.
	zone.NewGlobal()

	// WithMouseCellMotion enables click + scroll wheel events. CellMotion
	// is cheaper than AllMotion (events only on cell boundaries) and
	// sufficient for click-to-select + wheel scroll.
	//
	// bubbletea traps SIGINT and SIGTERM itself but not SIGHUP, whose
	// default action kills the process on the spot: the terminal would be
	// left in the alt screen with mouse reporting on, and librespot would
	// be orphaned, still advertising itself as a Connect device. Turning
	// SIGHUP into a cancelled program context lets Run return normally so
	// the terminal is restored and every deferred cleanup runs.
	hupCtx, stopHup := signal.NotifyContext(ctx, syscall.SIGHUP)
	defer stopHup()
	p := tea.NewProgram(
		ui.NewModel(ctx, session.Client, opts...),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(hupCtx),
	)
	_, err = p.Run()
	// Cancel now rather than via defer: the deferred cleanups (librespot
	// Stop can wait up to 5s) run before a deferred cancel would, so
	// in-flight Cmds, the reconnect handler and token refresh would keep
	// running against a live context during teardown. The deferred cancel
	// above still covers the early-return paths.
	cancel()
	if tokenRevoked.Load() {
		// Replace any tea.Run error (likely nil — the UI returned
		// tea.Quit cleanly) with a specific re-login message. The
		// stale token file was auto-deleted in auth.signalRevoked, but
		// include the path so the user can verify or remove manually
		// if the delete silently failed (read-only fs, permissions).
		path := "~/.config/tuify/token.json"
		if dir, derr := config.Dir(); derr == nil {
			path = filepath.Join(dir, "token.json")
		}
		return fmt.Errorf(
			"Spotify refresh token was revoked.\n"+
				"Restart tuify to re-authenticate.\n"+
				"If login still fails, delete %s and try again.",
			path,
		)
	}
	switch {
	case errors.Is(err, tea.ErrInterrupted):
		// SIGINT from outside (Ctrl+C inside the TUI is a key press and
		// quits cleanly). Not a failure worth an error message.
		return ErrInterrupted
	case errors.Is(err, tea.ErrProgramKilled) && hupCtx.Err() != nil:
		return nil // terminal hung up; cleanup has run
	}
	return err
}
