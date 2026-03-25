package librespot

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds librespot launch parameters.
type Config struct {
	BinaryPath  string // path to librespot binary, default "librespot"
	DeviceName  string // Spotify Connect device name, default "tuify"
	Bitrate     int    // 96, 160, or 320; default 320
	AudioWorker string // full command for subprocess backend
	Username    string // Spotify username for direct auth (avoids zeroconf key issues)
}

func (c *Config) setDefaults() {
	if c.BinaryPath == "" {
		c.BinaryPath = "librespot"
	}
	if c.DeviceName == "" {
		c.DeviceName = "tuify"
	}
	if c.Bitrate == 0 {
		c.Bitrate = 320
	}
}

// Process manages a librespot child process with automatic restart on crash.
type Process struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	config   Config
	done     chan struct{} // closed when process exits (per launch)
	stopCh   chan struct{} // closed when Stop() is called to suppress restart
	stopped  bool

	// Broken session detection: track repeated audio key timeouts that
	// indicate librespot reconnected but is stuck in a non-functional state.
	audioKeyErrCount int
	firstAudioKeyErr time.Time
}

// NewProcess creates a new Process with the given configuration.
func NewProcess(cfg Config) *Process {
	cfg.setDefaults()
	return &Process{
		config: cfg,
		stopCh: make(chan struct{}),
	}
}

// Args returns the librespot command-line arguments.
func (p *Process) Args() []string {
	args := []string{
		"--name", p.config.DeviceName,
		"--backend", "subprocess",
		"--device", p.config.AudioWorker,
		"--bitrate", strconv.Itoa(p.config.Bitrate),
		"--initial-volume", "60",
		"--volume-ctrl", "fixed",
		"--disable-audio-cache",
	}
	if p.config.Username != "" {
		args = append(args, "--username", p.config.Username)
	}
	return args
}

// Start launches the librespot process. If the process crashes, it will be
// automatically restarted with backoff proportional to how quickly it died
// (2s–30s). The delay resets after 60 seconds of stable uptime.
func (p *Process) Start() error {
	return p.launch()
}

// launch starts the underlying OS process (no restart logic here).
func (p *Process) launch() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return fmt.Errorf("librespot already running")
	}
	if p.stopped {
		return fmt.Errorf("librespot process has been stopped")
	}

	args := p.Args()
	p.cmd = exec.Command(p.config.BinaryPath, args...)
	p.done = make(chan struct{})

	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to get librespot stdout: %w", err)
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to get librespot stderr: %w", err)
	}

	log.Printf("[librespot] starting: %s %v", p.config.BinaryPath, args)

	if err := p.cmd.Start(); err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to start librespot: %w", err)
	}

	p.audioKeyErrCount = 0

	go pipeLog("[librespot:out]", stdout, nil)
	go pipeLog("[librespot:err]", stderr, p.monitorStderr)

	startedAt := time.Now()
	done := p.done

	go func() {
		err := p.cmd.Wait()
		if err != nil {
			log.Printf("[librespot] exited: %v", err)
		} else {
			log.Printf("[librespot] exited normally")
		}
		p.mu.Lock()
		p.cmd = nil
		p.mu.Unlock()
		close(done)

		p.scheduleRestart(startedAt)
	}()

	return nil
}

const (
	restartBaseDelay = 2 * time.Second
	restartMaxDelay  = 30 * time.Second
	stableThreshold  = 60 * time.Second
	stopTimeout      = 5 * time.Second

	audioKeyErrWindow    = 60 * time.Second
	audioKeyErrThreshold = 2
)

// scheduleRestart handles automatic restart with linear backoff.
func (p *Process) scheduleRestart(lastStart time.Time) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	// Determine backoff delay based on how long the process was alive.
	// The faster it died, the longer we wait (up to restartMaxDelay).
	// If it ran longer than stableThreshold, restart quickly.
	uptime := time.Since(lastStart)
	delay := restartBaseDelay
	if uptime < stableThreshold {
		ratio := float64(stableThreshold-uptime) / float64(stableThreshold)
		delay = time.Duration(float64(restartMaxDelay) * ratio)
		if delay < restartBaseDelay {
			delay = restartBaseDelay
		}
	}

	log.Printf("[librespot] restarting in %v (uptime was %v)", delay.Round(time.Second), uptime.Round(time.Second))

	select {
	case <-time.After(delay):
	case <-p.stopCh:
		return
	}

	if err := p.launch(); err != nil {
		log.Printf("[librespot] restart failed: %v", err)
	}
}

// Stop sends SIGTERM, waits up to 5 seconds, then SIGKILL.
// Suppresses any pending or future automatic restarts.
func (p *Process) Stop() error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.stopped = true
	close(p.stopCh)
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil || done == nil {
		return nil
	}

	log.Printf("[librespot] stopping")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}

	select {
	case <-done:
	case <-time.After(stopTimeout):
		log.Printf("[librespot] force killing after %v timeout", stopTimeout)
		_ = cmd.Process.Kill()
		<-done
	}

	return nil
}

// monitorStderr checks for repeated audio key timeouts that indicate a broken
// session (librespot reconnected internally but can't decrypt audio). After
// seeing audioKeyErrThreshold timeouts within audioKeyErrWindow, we force-kill
// the process so the auto-restart logic can start a clean session.
func (p *Process) monitorStderr(line string) {
	if !strings.Contains(line, "Audio key response timeout") {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil {
		return
	}

	now := time.Now()
	if now.Sub(p.firstAudioKeyErr) > audioKeyErrWindow {
		p.audioKeyErrCount = 0
		p.firstAudioKeyErr = now
	}
	p.audioKeyErrCount++

	if p.audioKeyErrCount >= audioKeyErrThreshold {
		log.Printf("[librespot] %d audio key timeouts in %v — killing for clean restart",
			p.audioKeyErrCount, now.Sub(p.firstAudioKeyErr).Round(time.Second))
		p.audioKeyErrCount = 0
		_ = p.cmd.Process.Kill()
	}
}

// pipeLog reads lines from r and writes them to the log with the given prefix.
// Filters out noisy libmdns warnings. If onLine is non-nil, it is called for
// each non-filtered line.
func pipeLog(prefix string, r io.Reader, onLine func(string)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "libmdns::fsm") {
			continue
		}
		log.Printf("%s %s", prefix, line)
		if onLine != nil {
			onLine(line)
		}
	}
}
