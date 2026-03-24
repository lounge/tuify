package librespot

import (
	"bufio"
	"fmt"
	"io"
	"log"
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

// Process manages a librespot child process.
type Process struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	config Config
	done   chan struct{}
}

// NewProcess creates a new Process with the given configuration.
func NewProcess(cfg Config) *Process {
	cfg.setDefaults()
	return &Process{config: cfg}
}

// Args returns the librespot command-line arguments.
func (p *Process) Args() []string {
	args := []string{
		"--name", p.config.DeviceName,
		"--backend", "subprocess",
		"--device", p.config.AudioWorker,
		"--bitrate", strconv.Itoa(p.config.Bitrate),
		"--initial-volume", "100",
		"--volume-ctrl", "fixed",
		"--disable-audio-cache",
	}
	if p.config.Username != "" {
		args = append(args, "--username", p.config.Username)
	}
	return args
}

// Start launches the librespot process.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return fmt.Errorf("librespot already running")
	}

	args := p.Args()
	p.cmd = exec.Command(p.config.BinaryPath, args...)
	p.done = make(chan struct{})

	// Capture stdout and stderr so we can see librespot's logs and subprocess output.
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

	go pipeLog("[librespot:out]", stdout)
	go pipeLog("[librespot:err]", stderr)

	go func() {
		err := p.cmd.Wait()
		if err != nil {
			log.Printf("[librespot] exited: %v", err)
		} else {
			log.Printf("[librespot] exited normally")
		}
		close(p.done)
	}()

	return nil
}

// Stop sends SIGTERM, waits up to 5 seconds, then SIGKILL.
func (p *Process) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	log.Printf("[librespot] stopping")

	if err := cmd.Process.Kill(); err != nil {
		return err
	}

	select {
	case <-done:
		// Process exited.
	case <-time.After(5 * time.Second):
		log.Printf("[librespot] force killing after 5s timeout")
		_ = cmd.Process.Kill()
		<-done
	}

	p.mu.Lock()
	p.cmd = nil
	p.mu.Unlock()

	return nil
}

// Running returns true if the process is alive.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// DeviceName returns the configured Spotify Connect device name.
func (p *Process) DeviceName() string {
	return p.config.DeviceName
}

// pipeLog reads lines from r and writes them to the log with the given prefix.
// Filters out noisy libmdns warnings.
func pipeLog(prefix string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "libmdns::fsm") {
			continue
		}
		log.Printf("%s %s", prefix, line)
	}
}
