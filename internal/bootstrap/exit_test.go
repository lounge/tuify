package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// panicModel panics in whichever entry point names it; otherwise it echoes
// the message it was given through the command it returns.
type panicModel struct {
	in    string
	batch bool
}

func (m panicModel) Init() tea.Cmd {
	if m.in == "Init" {
		panic("boom in Init")
	}
	return nil
}

func (m panicModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.in == "Update" {
		panic("boom in Update")
	}
	cmd := func() tea.Msg {
		if m.in == "command" {
			panic("boom in command")
		}
		return msg
	}
	if m.batch {
		return m, tea.Batch(cmd, cmd)
	}
	return m, cmd
}

func (m panicModel) View() string {
	if m.in == "View" {
		panic("boom in View")
	}
	return "view"
}

// captureLog redirects the standard logger for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// mustRepanic runs f and returns the value it panicked with.
func mustRepanic(t *testing.T, f func()) (r any) {
	t.Helper()
	defer func() { r = recover() }()
	f()
	t.Fatal("expected the panic to propagate to Bubble Tea")
	return nil
}

func TestPanicLogModel_LogsAndRepanics(t *testing.T) {
	cases := map[string]func(m panicLogModel){
		"Init":   func(m panicLogModel) { m.Init() },
		"Update": func(m panicLogModel) { m.Update("msg") },
		"View":   func(m panicLogModel) { _ = m.View() },
		"command": func(m panicLogModel) {
			_, cmd := m.Update("msg")
			cmd()
		},
	}
	for where, run := range cases {
		t.Run(where, func(t *testing.T) {
			buf := captureLog(t)
			m := panicLogModel{inner: panicModel{in: where}}
			r := mustRepanic(t, func() { run(m) })
			want := "boom in " + where
			if r != want {
				t.Fatalf("re-panicked with %v, want the original value %q", r, want)
			}
			out := buf.String()
			if !strings.Contains(out, "[tuify] panic in "+where+": "+want) {
				t.Fatalf("log lacks the panic line:\n%s", out)
			}
			// The stack is taken before unwinding, so it names the
			// function that panicked.
			if !strings.Contains(out, "panicModel") {
				t.Fatalf("logged stack does not reach the panic site:\n%s", out)
			}
		})
	}
}

func TestPanicLogModel_WrapsCommandsInsideBatch(t *testing.T) {
	buf := captureLog(t)
	m := panicLogModel{inner: panicModel{in: "command", batch: true}}
	_, cmd := m.Update("msg")
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("batch not passed through: %#v", batch)
	}
	r := mustRepanic(t, func() { batch[0]() })
	if r != "boom in command" {
		t.Fatalf("re-panicked with %v", r)
	}
	if !strings.Contains(buf.String(), "[tuify] panic in command") {
		t.Fatalf("panic inside a batch was not logged:\n%s", buf.String())
	}
}

func TestPanicLogModel_PassesThrough(t *testing.T) {
	buf := captureLog(t)
	var m tea.Model = panicLogModel{inner: panicModel{}}
	if m.Init() != nil {
		t.Fatal("nil Init command must stay nil")
	}
	m, cmd := m.Update("hello")
	if got := cmd(); got != "hello" {
		t.Fatalf("command returned %v, want the inner command's message", got)
	}
	if _, ok := m.(panicLogModel); !ok {
		t.Fatalf("Update returned %T, want the wrapper so later calls stay covered", m)
	}
	if m.View() != "view" {
		t.Fatal("View not passed through")
	}
	if buf.Len() != 0 {
		t.Fatalf("logged without a panic: %s", buf.String())
	}
}

func TestExitReason(t *testing.T) {
	panicErr := fmt.Errorf("%w: %w", tea.ErrProgramKilled, tea.ErrProgramPanic)
	cases := []struct {
		name                        string
		err                         error
		revoked, hungUp, terminated bool
		want                        string
	}{
		{"quit", nil, false, false, false, "quit"},
		{"panic", panicErr, false, false, false, "panic (stack logged above)"},
		{"panic wins over hangup", panicErr, false, true, false, "panic (stack logged above)"},
		{"revoked", nil, true, false, false, "refresh token revoked"},
		{"sigint", tea.ErrInterrupted, false, false, false, "interrupted (SIGINT)"},
		{"sighup", tea.ErrProgramKilled, false, true, false, "terminal hung up (SIGHUP)"},
		{"sigterm", nil, false, false, true, "terminated (SIGTERM)"},
		{"other error", errors.New("tty gone"), false, false, false, "error: tty gone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitReason(tc.err, tc.revoked, tc.hungUp, tc.terminated); got != tc.want {
				t.Fatalf("exitReason = %q, want %q", got, tc.want)
			}
		})
	}
}
