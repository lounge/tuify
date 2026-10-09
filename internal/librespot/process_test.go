package librespot

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
	p := NewProcess(Config{})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.restartDelay(tc.uptime)
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

// logBuffer is a plain log sink. Unlike slowLog it does not sleep, which
// inside a synctest bubble would move the fake clock between the retries
// being counted.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A launch that fails must not end the restart cycle: scheduleRestart
// keeps retrying at the maximum delay until Stop closes stopCh. Runs in a
// synctest bubble so each retry is one tick of the fake clock and the
// number of attempts is exact; the binary path does not exist, so launch
// fails at Start without spawning anything.
//
// Not parallel: it redirects the global logger.
func TestScheduleRestart_RetriesFailedLaunchUntilStop(t *testing.T) {
	var logs logBuffer
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	synctest.Test(t, func(t *testing.T) {
		p := NewProcess(Config{BinaryPath: filepath.Join(t.TempDir(), "missing-librespot")})
		p.restartBaseDelay = 10 * time.Millisecond
		p.restartMaxDelay = 10 * time.Millisecond

		returned := make(chan struct{})
		go func() {
			p.scheduleRestart(time.Now())
			close(returned)
		}()

		const attempts = 3
		for range attempts {
			synctest.Sleep(p.restartMaxDelay)
		}
		failed := strings.Count(logs.String(), "restart failed")

		p.Stop()
		synctest.Wait()
		select {
		case <-returned:
		default:
			t.Error("scheduleRestart still running after Stop")
		}
		if failed != attempts {
			t.Errorf("launch attempted %d times before Stop, want %d:\n%s", failed, attempts, logs.String())
		}
	})
}

// --- child process tests ---

// fakeLibrespot writes body as an executable shell script and returns its
// path. Tests that use it are skipped on Windows.
func fakeLibrespot(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake librespot")
	}
	script := filepath.Join(t.TempDir(), "librespot")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o700); err != nil { //nolint:gosec // the fake binary must be executable
		t.Fatal(err)
	}
	return script
}

// fastProcess returns a Process for script with millisecond restart and
// stop timings, so the restart cycle and force-kill path run in a test.
func fastProcess(t *testing.T, script string) *Process {
	t.Helper()
	p := NewProcess(Config{BinaryPath: script, Backend: "pulseaudio"})
	p.restartBaseDelay = 10 * time.Millisecond
	p.restartMaxDelay = 10 * time.Millisecond
	p.stopTimeout = 20 * time.Millisecond
	t.Cleanup(p.Stop)
	return p
}

// countLaunches wires OnStdout, which launch calls once per child, to
// count launches. Each call drains the pipe so the child can never block
// on a full stdout.
func countLaunches(p *Process) <-chan struct{} {
	launched := make(chan struct{}, 64)
	p.OnStdout = func(r io.ReadCloser) {
		launched <- struct{}{}
		_, _ = io.Copy(io.Discard, r)
		r.Close()
	}
	return launched
}

// awaitLaunches fails the test unless n launches are observed within 2s.
func awaitLaunches(t *testing.T, launched <-chan struct{}, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := range n {
		select {
		case <-launched:
		case <-deadline:
			t.Fatalf("saw %d launches, want %d", i, n)
		}
	}
}

// A child that exits is relaunched, and again after that: the restart
// cycle is a loop, not a one-shot.
func TestRestart_RelaunchesExitedChild(t *testing.T) {
	t.Parallel()

	p := fastProcess(t, fakeLibrespot(t, "exit 1\n"))
	launched := countLaunches(p)

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	awaitLaunches(t, launched, 3)

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return")
	}
}

// The broken-session detector kills the child from the stderr reader, and
// the exit it causes goes through the normal restart cycle.
func TestMonitorStderr_KillsBrokenSessionChild(t *testing.T) {
	t.Parallel()

	// The child reports a broken session, then would live for 30s: only a
	// kill gets it relaunched within the test's deadline. exec hands the
	// pipes straight to sleep, so there is no orphan holding them open.
	script := fakeLibrespot(t, "echo 'Audio key response timeout' >&2\necho 'Spirc shut down unexpectedly' >&2\nexec sleep 30\n")
	p := fastProcess(t, script)
	launched := countLaunches(p)

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	awaitLaunches(t, launched, 2)
}

// Stop escalates to SIGKILL when the child ignores the interrupt, and
// returns once it is gone. Elapsed time is the observable: nothing short of
// the kill ends a child that ignores SIGINT and sleeps for 30s, and the
// kill cannot happen before the stop timeout.
func TestStop_ForceKillsChildThatIgnoresInterrupt(t *testing.T) {
	t.Parallel()

	// The trap survives exec (ignored signals are inherited), so sleep
	// itself ignores SIGINT; "ready" on stdout says the trap is in place
	// before Stop sends anything.
	script := fakeLibrespot(t, "trap '' INT\necho ready\nexec sleep 30\n")
	p := fastProcess(t, script)
	ready := make(chan struct{}, 1)
	p.OnStdout = func(r io.ReadCloser) {
		buf := make([]byte, 1)
		for {
			if _, err := r.Read(buf); err != nil || buf[0] == '\n' {
				break
			}
		}
		ready <- struct{}{}
		_, _ = io.Copy(io.Discard, r)
		r.Close()
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("fake librespot never reported ready")
	}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return: the child was not force-killed")
	}
	if elapsed := time.Since(start); elapsed < p.stopTimeout {
		t.Errorf("Stop returned after %v, before the %v stop timeout: the child died from SIGINT, so force-kill was not exercised", elapsed, p.stopTimeout)
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

// slowLog is a log sink that takes a moment per line, standing in for a
// busy machine, so librespot exits while its output is still being read.
type slowLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *slowLog) Write(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *slowLog) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The output librespot writes right before it dies is what explains the
// crash, so it must be logged before the exit is. Wait used to close the
// stderr pipe while it was still being read and drop that tail.
//
// Not parallel: it redirects the global logger.
func TestLaunch_LogsOutputWrittenBeforeExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake librespot")
	}
	script := filepath.Join(t.TempDir(), "librespot")
	body := "#!/bin/sh\ni=0\nwhile [ $i -lt 300 ]; do echo \"line $i\" >&2; i=$((i+1)); done\necho 'FATAL: last words' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // the fake binary must be executable
		t.Fatal(err)
	}

	var logs slowLog
	prev := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	p := NewProcess(Config{BinaryPath: script, Backend: "pulseaudio"})
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fake librespot did not exit")
	}
	p.Stop() // cancel the scheduled restart

	out := logs.String()
	last := strings.Index(out, "FATAL: last words")
	exited := strings.Index(out, "[librespot] exited")
	if last < 0 {
		t.Fatalf("last stderr line was never logged:\n%s", out[max(0, len(out)-500):])
	}
	if exited < last {
		t.Error("exit was logged before librespot's final output")
	}
	if strings.Contains(out, "file already closed") {
		t.Error("stderr reader hit a closed pipe")
	}
}
