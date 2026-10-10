package bootstrap

import (
	"errors"
	"log"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"
)

// panicLogModel wraps the UI model so a panic in Init, Update, View or a
// command they return is written to debug.log with its stack before it
// propagates. Bubble Tea still recovers it and restores the terminal, but
// it prints the panic only to the terminal, which is gone once the
// alternate screen closes or the window is cleared; debug.log is where a
// crash gets diagnosed afterwards.
type panicLogModel struct {
	inner tea.Model
}

func (m panicLogModel) Init() tea.Cmd {
	defer logPanic("Init")
	return wrapCmd(m.inner.Init())
}

func (m panicLogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer logPanic("Update")
	inner, cmd := m.inner.Update(msg)
	return panicLogModel{inner: inner}, wrapCmd(cmd)
}

func (m panicLogModel) View() string {
	defer logPanic("View")
	return m.inner.View()
}

// wrapCmd returns cmd with panic logging around it. A command that returns
// a tea.BatchMsg (tea.Batch) has the commands inside wrapped too, since
// Bubble Tea runs those itself.
func wrapCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		defer logPanic("command")
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			wrapped := make(tea.BatchMsg, len(batch))
			for i, c := range batch {
				wrapped[i] = wrapCmd(c)
			}
			return wrapped
		}
		return msg
	}
}

// logPanic logs a panic in progress, with its stack, and panics again with
// the same value so Bubble Tea's own recovery still runs. Deferred directly,
// so recover sees the panic.
func logPanic(where string) {
	r := recover()
	if r == nil {
		return
	}
	log.Printf("[tuify] panic in %s: %v\n%s", where, r, debug.Stack())
	panic(r)
}

// exitReason describes why the TUI returned, for the one line Run logs
// before shutdown. err is p.Run's error; revoked, hungUp and terminated
// report the refresh-token revocation, SIGHUP and SIGTERM. A quit key logs
// its own line in the ui package, so a plain quit only says "quit".
func exitReason(err error, revoked, hungUp, terminated bool) string {
	switch {
	case errors.Is(err, tea.ErrProgramPanic):
		return "panic (stack logged above)"
	case revoked:
		return "refresh token revoked"
	case errors.Is(err, tea.ErrInterrupted):
		return "interrupted (SIGINT)"
	case hungUp:
		return "terminal hung up (SIGHUP)"
	case terminated:
		return "terminated (SIGTERM)"
	case err != nil:
		return "error: " + err.Error()
	default:
		return "quit"
	}
}
