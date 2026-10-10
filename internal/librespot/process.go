package librespot

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBackend is the audio backend used when none is configured:
	// raw PCM on stdout, which tuify reads for the visualizers and plays
	// itself.
	DefaultBackend = "pipe"
	// DefaultDeviceName is the Spotify Connect name librespot registers
	// under when none is configured.
	DefaultDeviceName = "tuify"
)

// Config holds librespot launch parameters.
type Config struct {
	BinaryPath string // path to librespot binary, default "librespot"
	DeviceName string // Spotify Connect device name, default DefaultDeviceName
	Bitrate    int    // 96, 160, or 320; default 320
	Backend    string // audio backend: DefaultBackend ("pipe"), "pulseaudio", etc.
	Username   string // Spotify username for direct auth (avoids zeroconf key issues)
	CacheDir   string // directory for librespot credential/audio cache
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
	mu      sync.Mutex
	cmd     *exec.Cmd
	config  Config
	done    chan struct{} // closed when process exits (per launch)
	stopCh  chan struct{} // closed when Stop() is called to suppress restart
	stopped bool

	// Broken session detection: an audio key timeout combined with spirc
	// shutdown (in either order) means librespot is stuck.
	sawAudioKeyErr bool
	sawSpirc       bool

	// Timing knobs, set to the package constants by NewProcess. Fields
	// rather than constants so tests can drive a real child process through
	// restart and force-kill in milliseconds.
	restartBaseDelay time.Duration
	restartMaxDelay  time.Duration
	stableThreshold  time.Duration
	stopTimeout      time.Duration

	OnReconnect func()              // called when librespot authenticates (initial or restart)
	OnInactive  func()              // called when librespot reports device became inactive
	OnStdout    func(io.ReadCloser) // called with the stdout pipe in launch(); for pipe backend audio
}

// NewProcess creates a new Process with the given configuration.
func NewProcess(cfg Config) *Process {
	cfg.setDefaults()
	return &Process{
		config:           cfg,
		stopCh:           make(chan struct{}),
		restartBaseDelay: restartBaseDelay,
		restartMaxDelay:  restartMaxDelay,
		stableThreshold:  stableThreshold,
		stopTimeout:      stopTimeout,
	}
}

// args returns the librespot command-line arguments.
func (p *Process) args() []string {
	args := []string{
		"--name", p.config.DeviceName,
		"--backend", p.config.Backend,
	}
	if p.config.CacheDir != "" {
		args = append(args, "--cache", p.config.CacheDir)
	}
	args = append(args,
		"--bitrate", strconv.Itoa(p.config.Bitrate),
		"--initial-volume", "60",
		// Linear softvol: PCM amplitude scales 1:1 with the slider, so the
		// inverse gain applied in audio.PipeReader.Latest() mathematically
		// restores the pre-volume FFT bands. "log" is more natural for
		// listening but compresses the signal too hard for visualizers.
		"--volume-ctrl", "linear",
		"--disable-audio-cache",
	)
	if p.config.Username != "" {
		args = append(args, "--username", p.config.Username)
	}
	return args
}

// credentialFlags are the librespot flags whose value identifies or
// authenticates the user; redactArgs hides their values in the log.
var credentialFlags = map[string]bool{
	"--username": true, "-u": true,
	"--password": true, "-p": true,
	"--access-token": true, "-k": true,
}

// redactArgs returns a copy of args for logging, with the value after
// each credential flag replaced, so a debug.log attached to a bug report
// does not carry the user's Spotify account. args itself is unchanged.
func redactArgs(args []string) []string {
	out := slices.Clone(args)
	for i := 0; i+1 < len(out); i++ {
		if credentialFlags[out[i]] {
			out[i+1] = "<redacted>"
			i++
		}
	}
	return out
}

// Start launches the librespot process. If the process crashes, it will be
// automatically restarted with backoff proportional to how quickly it died
// (2s–30s). The delay resets after 60 seconds of stable uptime.
func (p *Process) Start() error {
	return p.launch()
}

// errStopped is returned by launch once Stop has been called, so a restart
// that lost the race with Stop can tell that from a launch failure.
var errStopped = errors.New("librespot process has been stopped")

// launch starts the underlying OS process (no restart logic here).
func (p *Process) launch() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return fmt.Errorf("librespot already running")
	}
	if p.stopped {
		return errStopped
	}

	args := p.args()
	cmd := exec.Command(p.config.BinaryPath, args...)
	p.cmd = cmd
	bindToParent(cmd)
	p.done = make(chan struct{})
	// Bounds how long Wait keeps copying output after librespot exits, in
	// case a child it spawned (e.g. an --onevent hook) still holds the
	// pipes open.
	p.cmd.WaitDelay = logDrainTimeout

	// Log output goes through io.Pipes instead of StdoutPipe/StderrPipe.
	// Those are closed by Wait as soon as the process is reaped, dropping
	// whatever was still unread, typically the lines explaining a crash.
	// With an io.Pipe, Wait returns only after exec has copied everything
	// into it, and the logger drains it before the exit is logged.
	var logsDone []<-chan struct{}
	var logWriters []*io.PipeWriter
	logTo := func(prefix string, onLine func(string)) io.Writer {
		pr, pw := io.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			pipeLog(prefix, pr, onLine)
		}()
		logsDone = append(logsDone, done)
		logWriters = append(logWriters, pw)
		return pw
	}
	closeLogs := func() {
		for _, pw := range logWriters {
			pw.Close()
		}
	}

	var stdout io.ReadCloser
	if p.OnStdout != nil {
		var err error
		if stdout, err = p.cmd.StdoutPipe(); err != nil {
			p.cmd = nil
			return fmt.Errorf("failed to get librespot stdout: %w", err)
		}
	} else {
		p.cmd.Stdout = logTo("[librespot:out]", nil)
	}
	// The monitor learns which launch a line belongs to. Its pipeLog
	// goroutine can outlive the process (drainLogs stops waiting after
	// logDrainTimeout), and a late line from the dead process must not kill
	// the next child or announce a reconnect for it.
	p.cmd.Stderr = logTo("[librespot:err]", func(line string) { p.monitorStderr(cmd, line) })

	log.Printf("[librespot] starting: %s %v", p.config.BinaryPath, redactArgs(args))

	if err := p.cmd.Start(); err != nil {
		if stdout != nil {
			stdout.Close()
		}
		closeLogs()
		p.cmd = nil
		return fmt.Errorf("failed to start librespot: %w", err)
	}

	p.sawAudioKeyErr = false
	p.sawSpirc = false

	if p.OnStdout != nil {
		go p.OnStdout(stdout)
	}

	startedAt := time.Now()
	done := p.done
	// The goroutine keeps its own cmd reference instead of reading p.cmd
	// without the lock; p.cmd is only cleared below, but that invariant is
	// invisible to the race detector and easy to break.

	go func() {
		err := cmd.Wait()
		closeLogs()
		drainLogs(logsDone)
		if err != nil {
			log.Printf("[librespot] exited: %v", err)
		} else {
			log.Printf("[librespot] exited normally")
		}
		p.mu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
		}
		p.mu.Unlock()
		close(done)

		p.scheduleRestart(startedAt)
	}()

	return nil
}

const (
	// maxLogLine caps one line of librespot output. bufio.Scanner's default
	// is 64 KiB; librespot occasionally dumps large debug payloads.
	maxLogLine = 1024 * 1024
	// logDrainTimeout bounds how long an exit waits for librespot's last
	// output to be copied and logged.
	logDrainTimeout = 2 * time.Second

	restartBaseDelay = 2 * time.Second
	restartMaxDelay  = 30 * time.Second
	stableThreshold  = 60 * time.Second
	stopTimeout      = 5 * time.Second
)

// restartDelay returns the backoff delay based on how long the process was alive.
// The faster it died, the longer we wait (up to restartMaxDelay).
// If it ran longer than stableThreshold, restart quickly (restartBaseDelay).
func (p *Process) restartDelay(uptime time.Duration) time.Duration {
	if uptime >= p.stableThreshold {
		return p.restartBaseDelay
	}
	ratio := float64(p.stableThreshold-uptime) / float64(p.stableThreshold)
	delay := max(time.Duration(float64(p.restartMaxDelay)*ratio), p.restartBaseDelay)
	return delay
}

// scheduleRestart handles automatic restart with linear backoff. A launch
// that fails (binary missing, fork error) is retried at the maximum delay
// until Stop: giving up would leave tuify without a device for the rest of
// the session over what is often a transient condition.
func (p *Process) scheduleRestart(lastStart time.Time) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	uptime := time.Since(lastStart)
	delay := p.restartDelay(uptime)

	log.Printf("[librespot] restarting in %v (uptime was %v)", delay.Round(time.Second), uptime.Round(time.Second))

	for {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-p.stopCh:
			timer.Stop()
			return
		}

		err := p.launch()
		if err == nil {
			return
		}
		if errors.Is(err, errStopped) {
			// Stop landed between the timer firing and this launch. The
			// cycle is over; it is not a failed relaunch to retry.
			return
		}
		delay = p.restartMaxDelay
		log.Printf("[librespot] restart failed: %v (retrying in %v)", err, delay.Round(time.Second))
	}
}

// Stop sends an interrupt (SIGINT), waits up to 5 seconds, then SIGKILL.
// Suppresses any pending or future automatic restarts.
func (p *Process) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
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
		return
	}

	log.Printf("[librespot] stopping")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}

	select {
	case <-done:
	case <-time.After(p.stopTimeout):
		log.Printf("[librespot] force killing after %v timeout", p.stopTimeout)
		_ = cmd.Process.Kill()
		<-done
	}
}

// monitorStderr detects broken sessions where librespot reconnects internally
// but can't play audio. A kill is triggered when both an audio key timeout and
// a spirc shutdown are seen (in either order), or when "Unable to read audio
// file" follows an audio key timeout.
//
// from is the process that wrote line. Lines from any process other than
// the current one are ignored: after a restart the previous child's log
// reader may still be delivering its last output, and acting on it would
// kill the new child or fire OnReconnect for a session that is gone.
func (p *Process) monitorStderr(from *exec.Cmd, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if from != p.cmd {
		return
	}

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
//
// If scanning fails (e.g. a line longer than maxLogLine), the error is logged
// and the rest of r is discarded rather than abandoned: an undrained pipe
// would block librespot on its next write to stdout/stderr.
// drainLogs waits for the log readers to finish the output librespot wrote
// before exiting, giving up after logDrainTimeout so a stuck reader can't
// hold up the restart.
func drainLogs(done []<-chan struct{}) {
	timeout := time.After(logDrainTimeout)
	for _, d := range done {
		select {
		case <-d:
		case <-timeout:
			return
		}
	}
}

func pipeLog(prefix string, r io.Reader, onLine func(string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLogLine)
	defer func() {
		if err := scanner.Err(); err != nil {
			log.Printf("%s log scanning stopped, discarding further output: %v", prefix, err)
			_, _ = io.Copy(io.Discard, r)
		}
	}()
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
