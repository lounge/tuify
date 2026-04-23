package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/ui"
	zone "github.com/lrstanley/bubblezone"
)

// Run is the main application entry point. It loads config, authenticates,
// starts services, and runs the TUI. Returns an error on startup or runtime
// failure.
//
// The supporting pieces live in sibling files: setup.go (log/config/runtime
// resolve), auth_session.go (Spotify auth), librespot.go (optional local
// playback subprocess + audio pipe).
func Run() error {
	closeLog := SetupLog()
	defer closeLog()

	// Root context for the whole app run. Cancelled on return so every
	// background goroutine (token refresh, device polling, librespot
	// reconnect ops) unwinds cleanly instead of lingering after exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := LoadOrSetupConfig(nil, nil)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	rc := ResolveRuntime(cfg)

	session, err := Authenticate(ctx, rc)
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
			if _, ok := <-session.RevokedCh; !ok {
				return
			}
			tokenRevoked.Store(true)
			uiCh <- struct{}{}
		}()
		opts = append(opts, ui.WithTokenRevoked(uiCh))
	}

	svc, err := StartLibrespot(ctx, rc, session.Client)
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
	p := tea.NewProgram(
		ui.NewModel(ctx, session.Client, opts...),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
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
	return err
}
