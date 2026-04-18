package bootstrap

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
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
	return err
}
