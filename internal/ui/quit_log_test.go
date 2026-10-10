package ui

import (
	"bytes"
	"log"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A quit key must say so in debug.log: bootstrap sees a key quit and a
// SIGTERM alike, so this line is what tells them apart afterwards.
func TestQuitKey_LogsTheKey(t *testing.T) {
	cases := map[string]tea.KeyMsg{
		"q":      runeKey("q"),
		"ctrl+c": {Type: tea.KeyCtrlC},
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(prev) })

			_, cmd := pressKeys(t, newIntentTestModel(), key)
			if !isQuit(cmd) {
				t.Fatalf("%s did not quit", name)
			}
			if want := "[ui] quit: " + name + " pressed"; !strings.Contains(buf.String(), want) {
				t.Fatalf("log = %q, want it to contain %q", buf.String(), want)
			}
		})
	}
}
