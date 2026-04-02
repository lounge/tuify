package librespot

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestConfigSetDefaults(t *testing.T) {
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
	assertContains(t, args, "--volume-ctrl", "fixed")
	assertHasFlag(t, args, "--disable-audio-cache")

	// --device should NOT be present for pipe backend.
	for _, a := range args {
		if a == "--device" {
			t.Error("--device should not be present for pipe backend")
		}
	}
}

func TestArgs_NonPipeBackend(t *testing.T) {
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
	p := NewProcess(Config{Backend: "rodio"})
	args := p.args()

	for _, a := range args {
		if a == "--cache" {
			t.Error("--cache should not be present when CacheDir is empty")
		}
	}
}

func TestArgs_NoUsername(t *testing.T) {
	p := NewProcess(Config{Backend: "rodio"})
	args := p.args()

	for _, a := range args {
		if a == "--username" {
			t.Error("--username should not be present when Username is empty")
		}
	}
}

func TestNewProcess_AppliesDefaults(t *testing.T) {
	p := NewProcess(Config{})

	if p.config.BinaryPath != "librespot" {
		t.Errorf("BinaryPath: got %q", p.config.BinaryPath)
	}
	if p.config.Bitrate != 320 {
		t.Errorf("Bitrate: got %d", p.config.Bitrate)
	}
}

func TestMonitorStderr_AuthenticatedCallsOnReconnect(t *testing.T) {
	p := NewProcess(Config{})

	done := make(chan struct{})
	p.OnReconnect = func() { close(done) }

	p.monitorStderr("Authenticated as user@example.com")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnReconnect not called within 1s")
	}
}

func TestMonitorStderr_AudioKeyAndSpirc(t *testing.T) {
	p := NewProcess(Config{})

	// Feed audio key error first
	p.monitorStderr("Audio key response timeout")
	if !p.sawAudioKeyErr {
		t.Error("sawAudioKeyErr should be true")
	}

	// Then spirc shutdown — should trigger kill (but no process, so flags just reset)
	p.monitorStderr("Spirc shut down unexpectedly")
	if p.sawAudioKeyErr || p.sawSpirc {
		t.Error("flags should be reset after detection")
	}
}

func TestMonitorStderr_AudioKeyAndPlaybackFailure(t *testing.T) {
	p := NewProcess(Config{})

	p.monitorStderr("Audio key response timeout")
	p.monitorStderr("Unable to read audio file")

	if p.sawAudioKeyErr {
		t.Error("sawAudioKeyErr should be reset after detection")
	}
}

func TestMonitorStderr_NoFalsePositive(t *testing.T) {
	p := NewProcess(Config{})

	// Only spirc without audio key shouldn't trigger
	p.monitorStderr("Spirc shut down unexpectedly")
	if !p.sawSpirc {
		t.Error("sawSpirc should be set")
	}
	if p.sawAudioKeyErr {
		t.Error("sawAudioKeyErr should not be set")
	}
}

func TestPipeLog_FiltersLibmdns(t *testing.T) {
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
	input := "hello\nworld\n"
	r := strings.NewReader(input)

	// Should not panic with nil onLine
	pipeLog("[test]", r, nil)
}

func TestPipeLog_EmptyInput(t *testing.T) {
	r := bytes.NewReader(nil)

	var lines []string
	pipeLog("[test]", r, func(line string) {
		lines = append(lines, line)
	})

	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestStopIdempotent(t *testing.T) {
	p := NewProcess(Config{})

	// Stop without ever starting should be safe
	if err := p.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// --- restartDelay tests ---

func TestRestartDelay_ImmediateCrash(t *testing.T) {
	// Process died instantly — should get max delay.
	delay := restartDelay(0)
	if delay != restartMaxDelay {
		t.Errorf("expected %v for instant crash, got %v", restartMaxDelay, delay)
	}
}

func TestRestartDelay_StableUptime(t *testing.T) {
	// Process ran longer than stable threshold — should get base delay.
	delay := restartDelay(stableThreshold + time.Second)
	if delay != restartBaseDelay {
		t.Errorf("expected %v for stable process, got %v", restartBaseDelay, delay)
	}
}

func TestRestartDelay_ExactThreshold(t *testing.T) {
	// Process ran exactly at the stable threshold — should get base delay.
	delay := restartDelay(stableThreshold)
	if delay != restartBaseDelay {
		t.Errorf("expected %v at threshold, got %v", restartBaseDelay, delay)
	}
}

func TestRestartDelay_HalfUptime(t *testing.T) {
	// Process lived half the stable threshold — delay should be ~half of max.
	delay := restartDelay(stableThreshold / 2)
	expected := restartMaxDelay / 2
	tolerance := time.Second
	if delay < expected-tolerance || delay > expected+tolerance {
		t.Errorf("expected ~%v for half uptime, got %v", expected, delay)
	}
}

func TestRestartDelay_NearThreshold(t *testing.T) {
	// Process lived almost to threshold — delay should be close to base, but clamped.
	delay := restartDelay(stableThreshold - 100*time.Millisecond)
	if delay < restartBaseDelay {
		t.Errorf("delay %v should be >= base delay %v", delay, restartBaseDelay)
	}
}

// --- monitorStderr additional tests ---

func TestMonitorStderr_InactiveCallsOnInactive(t *testing.T) {
	p := NewProcess(Config{})

	done := make(chan struct{})
	p.OnInactive = func() { close(done) }

	p.monitorStderr("device became inactive")

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnInactive not called within 1s")
	}
}

func TestMonitorStderr_InactiveNilCallback(t *testing.T) {
	p := NewProcess(Config{})
	p.OnInactive = nil

	// Should not panic with nil OnInactive.
	p.monitorStderr("device became inactive")
}

func TestMonitorStderr_AuthenticatedResetsFlags(t *testing.T) {
	p := NewProcess(Config{})
	p.OnReconnect = func() {} // non-nil but no-op

	// Set broken session flags.
	p.sawAudioKeyErr = true
	p.sawSpirc = true

	p.monitorStderr("Authenticated as user@example.com")

	if p.sawAudioKeyErr {
		t.Error("sawAudioKeyErr should be reset on authentication")
	}
	if p.sawSpirc {
		t.Error("sawSpirc should be reset on authentication")
	}
}

func TestMonitorStderr_AuthenticatedNilCallback(t *testing.T) {
	p := NewProcess(Config{})
	p.OnReconnect = nil

	// Should not panic with nil OnReconnect.
	p.monitorStderr("Authenticated as user@example.com")
}

func TestMonitorStderr_UnrelatedLine(t *testing.T) {
	p := NewProcess(Config{})

	// Unrelated lines should not affect state.
	p.monitorStderr("Loading track xyz")
	if p.sawAudioKeyErr || p.sawSpirc {
		t.Error("unrelated line should not set flags")
	}
}

func TestMonitorStderr_SpircThenAudioKey(t *testing.T) {
	// Reverse order: spirc first, then audio key — should still trigger.
	p := NewProcess(Config{})

	p.monitorStderr("Spirc shut down unexpectedly")
	if !p.sawSpirc {
		t.Fatal("sawSpirc should be set")
	}

	p.monitorStderr("Audio key response timeout")
	// Now both flags should have been set and the detection should have reset them.
	if p.sawAudioKeyErr || p.sawSpirc {
		t.Error("flags should be reset after broken session detection")
	}
}

// --- scheduleRestart tests ---

func TestScheduleRestart_StopChSuppresses(t *testing.T) {
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
	for _, a := range args {
		if a == flag {
			return
		}
	}
	t.Errorf("expected args to contain %s, got %v", flag, args)
}
