package ui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type seekFireMsg struct {
	seq   int
	posMs int
}

type clipboardResultMsg struct{ err error }

// playbackResultMsg is used for all device-bound commands.
type playbackResultMsg struct {
	err  error
	seek bool // true for seek results (uses lighter post-action polling)
}

// LibrespotInactiveMsg is sent (via p.Send) when librespot reports that the
// device became inactive, indicating playback moved to another device.
type LibrespotInactiveMsg struct{}

// TokenSaveErrMsg is delivered when the auth layer fails to persist a
// refreshed OAuth token. The UI surfaces this as a visible warning because
// the in-memory token still works for the session — but the user will be
// forced to log in again on next restart, and without a signal they have
// no way to connect that to a fixable cause (permissions, disk full, etc.).
type TokenSaveErrMsg struct{ Err error }

// searchCtx captures the parts that differ between API search and local filter search.
type searchCtx struct {
	query    *string
	list     *list.Model
	close    func()
	play     func(list.Item) tea.Cmd
	onChange func() tea.Cmd
}
