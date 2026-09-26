package librespot

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestConfigSetDefaults(t *testing.T) {
	t.Parallel()

	c := Config{}
	c.setDefaults()

	if c.BinaryPath != "librespot" {
		t.Errorf("BinaryPath: got %q, want %q", c.BinaryPath, "librespot")
	}
	if c.DeviceName != DefaultDeviceName {
		t.Errorf("DeviceName: got %q, want %q", c.DeviceName, DefaultDeviceName)
	}
	if c.Bitrate != 320 {
		t.Errorf("Bitrate: got %d, want 320", c.Bitrate)
	}
	if c.Backend != DefaultBackend {
		t.Errorf("Backend: got %q, want %q", c.Backend, DefaultBackend)
	}
}

func TestConfigSetDefaults_Preserves(t *testing.T) {
	t.Parallel()

	c := Config{
		BinaryPath: "/custom/librespot",
		DeviceName: "custom",
		Bitrate:    160,
		Backend:    "pulseaudio",
	}
	c.setDefaults()

	if c.BinaryPath != "/custom/librespot" {
		t.Errorf("BinaryPath: got %q", c.BinaryPath)
	}
	if c.DeviceName != "custom" {
		t.Errorf("DeviceName: got %q", c.DeviceName)
	}
	if c.Bitrate != 160 {
		t.Errorf("Bitrate: got %d", c.Bitrate)
	}
	if c.Backend != "pulseaudio" {
		t.Errorf("Backend: got %q", c.Backend)
	}
}

func TestArgs_PipeBackend(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{
		DeviceName: "test-device",
		Backend:    DefaultBackend,
		Bitrate:    160,
		CacheDir:   "/tmp/cache",
		Username:   "user1",
	})

	args := p.args()

	assertContains(t, args, "--name", "test-device")
	assertContains(t, args, "--backend", "pipe")
	assertContains(t, args, "--cache", "/tmp/cache")
	assertContains(t, args, "--bitrate", "160")
	assertContains(t, args, "--username", "user1")
	assertContains(t, args, "--initial-volume", "60")
	assertContains(t, args, "--volume-ctrl", "linear")
	assertHasFlag(t, args, "--disable-audio-cache")

	// --device should NOT be present for pipe backend.
	for _, a := range args {
		if a == "--device" {
			t.Error("--device should not be present for pipe backend")
		}
	}
}

func TestArgs_NonPipeBackend(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{
		Backend: "pulseaudio",
	})

	args := p.args()

	// --device should NOT be present for any backend.
	for _, a := range args {
		if a == "--device" {
			t.Error("--device should not be present")
		}
	}
	assertContains(t, args, "--backend", "pulseaudio")
}

func TestArgs_NoCacheDir(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{Backend: "rodio"})
	args := p.args()

	for _, a := range args {
		if a == "--cache" {
			t.Error("--cache should not be present when CacheDir is empty")
		}
	}
}

func TestArgs_NoUsername(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{Backend: "rodio"})
	args := p.args()

	for _, a := range args {
		if a == "--username" {
			t.Error("--username should not be present when Username is empty")
		}
	}
}

func TestNewProcess_AppliesDefaults(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{})

	if p.config.BinaryPath != "librespot" {
		t.Errorf("BinaryPath: got %q", p.config.BinaryPath)
	}
	if p.config.Bitrate != 320 {
		t.Errorf("Bitrate: got %d", p.config.Bitrate)
	}
}

func TestPipeLog_FiltersLibmdns(t *testing.T) {
	t.Parallel()

	input := "line one\nlibmdns::fsm noisy line\nline three\n"
	r := strings.NewReader(input)

	var lines []string
	pipeLog("[test]", r, func(line string) {
		lines = append(lines, line)
	})

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (filtered libmdns), got %d: %v", len(lines), lines)
	}
	if lines[0] != "line one" || lines[1] != "line three" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestPipeLog_NilCallback(t *testing.T) {
	t.Parallel()

	input := "hello\nworld\n"
	r := strings.NewReader(input)

	// Should not panic with nil onLine
	pipeLog("[test]", r, nil)
}

func TestPipeLog_EmptyInput(t *testing.T) {
	t.Parallel()

	r := bytes.NewReader(nil)

	var lines []string
	pipeLog("[test]", r, func(line string) {
		lines = append(lines, line)
	})

	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestPipeLog_LineLongerThanDefaultScannerBuffer(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 100*1024) // over bufio's 64 KiB default
	r := strings.NewReader(long + "\nAuthenticated as user\n")

	var lines []string
	pipeLog("[test]", r, func(line string) { lines = append(lines, line) })

	if len(lines) != 2 || lines[1] != "Authenticated as user" {
		t.Fatalf("got %d lines, want the long line and the one after it", len(lines))
	}
}

// A line over maxLogLine stops scanning. pipeLog must keep draining the
// pipe afterwards, or the writer (librespot) would block forever.
func TestPipeLog_DrainsAfterScanError(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	writerDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(pw, strings.Repeat("x", maxLogLine+1)+"\n")
		if err == nil {
			_, err = io.WriteString(pw, "more output\n")
		}
		pw.Close()
		writerDone <- err
	}()

	pipeDone := make(chan struct{})
	go func() {
		pipeLog("[test]", pr, nil)
		close(pipeDone)
	}()

	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("writer: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer blocked: pipeLog stopped draining after the scan error")
	}
	select {
	case <-pipeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pipeLog did not return after the writer closed")
	}
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{})

	// Stop without ever starting, and a second Stop, must return
	// promptly without panicking (e.g. on a double close of stopCh).
	p.Stop()
	p.Stop()
}

// --- restartDelay tests ---

func TestRestartDelay(t *testing.T) {
	t.Parallel()

	// The delay scales linearly from restartMaxDelay for an instant crash
	// down to restartBaseDelay at stableThreshold, and never drops below
	// the base. The tolerance absorbs float rounding in the ratio.
	tests := []struct {
		name   string
		uptime time.Duration
		want   time.Duration
	}{
		{"immediate crash", 0, restartMaxDelay},
		{"one sixth uptime", stableThreshold / 6, restartMaxDelay * 5 / 6},
		{"half uptime", stableThreshold / 2, restartMaxDelay / 2},
		{"near threshold clamps to base", stableThreshold - 100*time.Millisecond, restartBaseDelay},
		{"exact threshold", stableThreshold, restartBaseDelay},
		{"stable uptime", stableThreshold + time.Second, restartBaseDelay},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := restartDelay(tc.uptime)
			if d := got - tc.want; d < -time.Millisecond || d > time.Millisecond {
				t.Errorf("restartDelay(%v) = %v, want %v", tc.uptime, got, tc.want)
			}
		})
	}
}

// --- monitorStderr tests ---

// TestMonitorStderr_BrokenSessionFlags drives the broken-session detector
// with sequences of stderr lines. An audio key timeout plus either a spirc
// shutdown (in any order) or a playback failure is a broken session, which
// resets both flags; authentication clears them. Callbacks are nil here,
// so this also covers monitorStderr tolerating nil OnReconnect/OnInactive.
func TestMonitorStderr_BrokenSessionFlags(t *testing.T) {
	t.Parallel()

	const (
		audioKey = "Audio key response timeout"
		spirc    = "Spirc shut down unexpectedly"
		playback = "Unable to read audio file"
		authed   = "Authenticated as user@example.com"
	)
	// Presetting a flag makes a false detection visible: detection clears
	// both flags, so a preset flag that survives proves nothing fired.
	tests := []struct {
		name                        string
		presetAudioKey, presetSpirc bool
		lines                       []string
		wantAudioKey, wantSpirc     bool
	}{
		{"audio key alone", false, false, []string{audioKey}, true, false},
		{"spirc alone is not broken", false, false, []string{spirc}, false, true},
		{"audio key then spirc", false, false, []string{audioKey, spirc}, false, false},
		{"spirc then audio key", false, false, []string{spirc, audioKey}, false, false},
		{"audio key then playback failure", false, false, []string{audioKey, playback}, false, false},
		{"playback failure without audio key is not broken", false, true, []string{playback}, false, true},
		{"unrelated line", false, true, []string{"Loading track xyz"}, false, true},
		{"authentication clears flags", true, true, []string{authed}, false, false},
		{"inactive leaves flags", true, true, []string{"device became inactive"}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProcess(Config{})
			p.sawAudioKeyErr, p.sawSpirc = tc.presetAudioKey, tc.presetSpirc
			for _, line := range tc.lines {
				p.monitorStderr(line)
			}
			if p.sawAudioKeyErr != tc.wantAudioKey || p.sawSpirc != tc.wantSpirc {
				t.Errorf("flags after %q: sawAudioKeyErr=%v sawSpirc=%v, want %v %v",
					tc.lines, p.sawAudioKeyErr, p.sawSpirc, tc.wantAudioKey, tc.wantSpirc)
			}
		})
	}
}

// TestMonitorStderr_Callbacks checks each callback line fires its callback.
// Callbacks run in their own goroutine, so the "other callback" check is
// best-effort: it can miss a wrong callback that fires late, but never
// flakes.
func TestMonitorStderr_Callbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		line          string
		wantReconnect bool
	}{
		{"authenticated calls OnReconnect", "Authenticated as user@example.com", true},
		{"inactive calls OnInactive", "device became inactive", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProcess(Config{})
			reconnect := make(chan struct{}, 1)
			inactive := make(chan struct{}, 1)
			p.OnReconnect = func() { reconnect <- struct{}{} }
			p.OnInactive = func() { inactive <- struct{}{} }

			p.monitorStderr(tc.line)

			want, other := inactive, reconnect
			if tc.wantReconnect {
				want, other = reconnect, inactive
			}
			select {
			case <-want:
			case <-time.After(time.Second):
				t.Fatal("callback not called within 1s")
			}
			select {
			case <-other:
				t.Error("the other callback fired too")
			default:
			}
		})
	}
}

// --- scheduleRestart tests ---

func TestScheduleRestart_StopChSuppresses(t *testing.T) {
	t.Parallel()

	p := NewProcess(Config{})

	// Close stopCh before scheduleRestart so it returns immediately.
	close(p.stopCh)
	p.stopped = true

	// Should return immediately without trying to launch.
	done := make(chan struct{})
	go func() {
		p.scheduleRestart(time.Now())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduleRestart did not return after stopCh closed")
	}
}

// helpers

func assertContains(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected args to contain %s %s, got %v", flag, value, args)
}

func assertHasFlag(t *testing.T, args []string, flag string) {
	t.Helper()
	if slices.Contains(args, flag) {
		return
	}
	t.Errorf("expected args to contain %s, got %v", flag, args)
}
