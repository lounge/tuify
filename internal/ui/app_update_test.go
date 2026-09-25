package ui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The waitFor* Cmds park on bootstrap channels that usually never fire.
// They must return once the root context is cancelled, and must not re-arm
// into a busy loop when their channel is closed.
func TestWaitCmds_ExitOnShutdownAndClosedChannel(t *testing.T) {
	cmds := []struct {
		name string
		cmd  func(Model) tea.Cmd
	}{
		{"librespotInactive", Model.waitForLibrespotInactive},
		{"tokenSaveErr", Model.waitForTokenSaveErr},
		{"tokenRevoked", Model.waitForTokenRevoked},
	}
	for _, tc := range cmds {
		t.Run(tc.name+"/shutdown", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			m := Model{
				rootCtx:             ctx,
				librespotInactiveCh: make(chan struct{}),
				tokenSaveErrCh:      make(chan error),
				tokenRevokedCh:      make(chan struct{}),
			}
			done := runCmd(tc.cmd(m))
			cancel()
			assertReturnsNil(t, done)
		})
		t.Run(tc.name+"/closed", func(t *testing.T) {
			libCh, saveCh, revCh := make(chan struct{}), make(chan error), make(chan struct{})
			close(libCh)
			close(saveCh)
			close(revCh)
			m := Model{
				rootCtx:             t.Context(),
				librespotInactiveCh: libCh,
				tokenSaveErrCh:      saveCh,
				tokenRevokedCh:      revCh,
			}
			assertReturnsNil(t, runCmd(tc.cmd(m)))
		})
	}
}

func runCmd(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	return done
}

func assertReturnsNil(t *testing.T, done <-chan tea.Msg) {
	t.Helper()
	select {
	case msg := <-done:
		if msg != nil {
			t.Errorf("got %T, want nil", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cmd did not return")
	}
}
