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

const (
	DefaultBackend    = "subprocess"
	DefaultDeviceName = "tuify"
)

// Config holds librespot launch parameters.
type Config struct {
	BinaryPath  string // path to librespot binary, default "librespot"
	DeviceName  string // Spotify Connect device name, default DefaultDeviceName
	Bitrate     int    // 96, 160, or 320; default 320
	Backend     string // audio backend: DefaultBackend, "pulseaudio", etc.
	AudioWorker string // full command for subprocess backend (only used when Backend == DefaultBackend)
	Username    string // Spotify username for direct auth (avoids zeroconf key issues)
	CacheDir    string // directory for librespot credential/audio cache
}

func (c *Config) setDefaults() {
	if c.BinaryPath == "" {
		c.BinaryPath = "librespot"
	}
	if c.DeviceName == "" {
		c.DeviceName = DefaultDeviceName
	}
	if c.Bitrate == 0 {
		c.Bitrate = 320
	}
	if c.Backend == "" {
		c.Backend = DefaultBackend
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

	// Broken session detection: an audio key timeout combined with spirc
	// shutdown (in either order) means librespot is stuck.
	sawAudioKeyErr bool
	sawSpirc       bool

	OnReconnect func() // called when librespot authenticates (initial or restart)
	OnInactive  func() // called when librespot reports device became inactive
}

// NewProcess creates a new Process with the given configuration.
func NewProcess(cfg Config) *Process {
	cfg.setDefaults()
	return &Process{
		config: cfg,
		stopCh: make(chan struct{}),
	}
}

// args returns the librespot command-line arguments.
func (p *Process) args() []string {
	args := []string{
		"--name", p.config.DeviceName,
		"--backend", p.config.Backend,
	}
	if p.config.Backend == DefaultBackend {
		args = append(args, "--device", p.config.AudioWorker)
	}
	if p.config.CacheDir != "" {
		args = append(args, "--cache", p.config.CacheDir)
	}
	args = append(args,
		"--bitrate", strconv.Itoa(p.config.Bitrate),
		"--initial-volume", "60",
		"--volume-ctrl", "fixed",
		"--disable-audio-cache",
	)
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

	args := p.args()
	p.cmd = exec.Command(p.config.BinaryPath, args...)
	p.done = make(chan struct{})

	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to get librespot stdout: %w", err)
	}
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		p.cmd = nil
		return fmt.Errorf("failed to get librespot stderr: %w", err)
	}

	log.Printf("[librespot] starting: %s %v", p.config.BinaryPath, args)

	if err := p.cmd.Start(); err != nil {
		p.cmd = nil
		return fmt.Errorf("failed to start librespot: %w", err)
	}

	p.sawAudioKeyErr = false
	p.sawSpirc = false

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
	p.mu.Unlock()

	// Re-read cmd/done under the lock. Setting stopped=true above prevents
	// any new launch(), and closing stopCh aborts pending restarts. Any
	// in-flight launch() that already passed the stopped check will complete
	// and update p.cmd/p.done before we can re-acquire the lock.
	p.mu.Lock()
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

// monitorStderr detects broken sessions where librespot reconnects internally
// but can't play audio. A kill is triggered when both an audio key timeout and
// a spirc shutdown are seen (in either order), or when "Unable to read audio
// file" follows an audio key timeout.
func (p *Process) monitorStderr(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if strings.Contains(line, "Authenticated as") {
		p.sawAudioKeyErr = false
		p.sawSpirc = false
		if p.OnReconnect != nil {
			go p.OnReconnect()
		}
		return
	}

	if strings.Contains(line, "device became inactive") {
		if p.OnInactive != nil {
			go p.OnInactive()
		}
		return
	}

	if strings.Contains(line, "Audio key response timeout") {
		p.sawAudioKeyErr = true
	}
	if strings.Contains(line, "Spirc shut down unexpectedly") {
		p.sawSpirc = true
	}

	var reason string
	switch {
	case p.sawAudioKeyErr && p.sawSpirc:
		reason = "audio key timeout + spirc shutdown"
	case p.sawAudioKeyErr && strings.Contains(line, "Unable to read audio file"):
		reason = "audio key timeout + playback failure"
	default:
		return
	}

	p.sawAudioKeyErr = false
	p.sawSpirc = false

	if p.cmd != nil && p.cmd.Process != nil {
		log.Printf("[librespot] broken session detected (%s) — killing for clean restart", reason)
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
