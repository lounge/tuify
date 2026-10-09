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

// playbackOp names the command a playbackResultMsg answers. The commands
// that flip now-playing state ahead of the reply (play/pause, shuffle)
// get a value of their own so a failure reverts that flip and no other:
// a failed Next must not undo the pause the user just made.
type playbackOp uint8

const (
	opPlayback  playbackOp = iota // play, next, previous, stop: nothing flipped ahead of the reply
	opSeek                        // clears seekPending; lighter post-action polling
	opPlayPause                   // reverts playing on failure
	opShuffle                     // reverts shuffling on failure
)

// playbackResultMsg is used for all device-bound commands.
type playbackResultMsg struct {
	err error
	op  playbackOp
}

// librespotInactiveMsg is sent (via p.Send) when librespot reports that the
// device became inactive, indicating playback moved to another device.
type librespotInactiveMsg struct{}

// tokenSaveErrMsg is delivered when the auth layer reports a non-fatal
// problem: most importantly a failure to persist a refreshed OAuth token,
// but also a failed token refresh at startup. The UI surfaces this as a
// visible warning. For a save failure the in-memory token still works for
// the session, but the user will be forced to log in again on next
// restart, and without a signal they have no way to connect that to a
// fixable cause (permissions, disk full, etc.).
type tokenSaveErrMsg struct{ Err error }

// tokenRevokedMsg is delivered when Spotify rejects the refresh token as
// permanently invalid (user revoked app access, expiry from inactivity,
// etc.). Every API call will fail from this point on, so the UI shuts
// down cleanly — bootstrap.Run() then prints a re-login message to
// stderr and exits.
type tokenRevokedMsg struct{}

// searchCtx captures the parts that differ between API search and local filter search.
type searchCtx struct {
	query    *string
	list     *list.Model
	close    func()
	play     func(list.Item) tea.Cmd
	retry    func() tea.Cmd // reload after a failed fetch; Enter on the error row
	onChange func() tea.Cmd
}
