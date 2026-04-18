package bootstrap

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/librespot"
)

// RuntimeConfig holds the resolved configuration with defaults applied.
// ResolvedRedirectURL and ResolvedDeviceName are the final values after
// applying defaults — use these instead of the raw Config fields.
type RuntimeConfig struct {
	*config.Config
	ResolvedRedirectURL string
	ResolvedDeviceName  string
}

// SetupLog configures the global logger to write to debug.log in the config
// directory. Returns a cleanup function that closes the log file. If the log
// file can't be opened (missing home dir, read-only fs, etc.) the reason is
// printed to stderr so subsequent debug sessions aren't blind to why log
// output is missing.
func SetupLog() func() {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuify: debug log disabled: %v\n", err)
		return func() {}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "tuify: debug log disabled: create %s: %v\n", dir, err)
		return func() {}
	}
	logPath := filepath.Join(dir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuify: debug log disabled: open %s: %v\n", logPath, err)
		return func() {}
	}
	log.SetOutput(f)
	return func() { f.Close() }
}

// LoadOrSetupConfig loads the config file. If no config exists, it runs
// first-time setup by prompting the user via the provided reader and writer.
// Pass nil for r/w to use os.Stdin/os.Stdout.
func LoadOrSetupConfig(r io.Reader, w io.Writer) (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg != nil {
		return cfg, nil
	}

	if r == nil {
		r = os.Stdin
	}
	if w == nil {
		w = os.Stdout
	}
	return runSetup(r, w)
}

func runSetup(r io.Reader, w io.Writer) (*config.Config, error) {
	reader := bufio.NewReader(r)

	fmt.Fprintln(w, "Welcome to tuify! Let's set up Spotify.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "1. Go to https://developer.spotify.com/dashboard")
	fmt.Fprintln(w, "2. Create an app with redirect URI: http://127.0.0.1:4444/callback")
	fmt.Fprintln(w, "3. Copy your Client ID")
	fmt.Fprintln(w)
	fmt.Fprint(w, "Enter your Client ID: ")

	clientID, _ := reader.ReadString('\n')
	clientID = strings.TrimSpace(clientID)

	if clientID == "" {
		return nil, fmt.Errorf("client ID is required")
	}

	cfg := &config.Config{ClientID: clientID}
	if err := config.Save(cfg); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintln(w, "Config saved!")
	fmt.Fprintln(w)
	return cfg, nil
}

// ResolveRuntime applies defaults to the raw config and returns a RuntimeConfig
// ready for use by the rest of the application.
func ResolveRuntime(cfg *config.Config) RuntimeConfig {
	rc := RuntimeConfig{Config: cfg}

	rc.ResolvedRedirectURL = cfg.RedirectURL
	if rc.ResolvedRedirectURL == "" {
		rc.ResolvedRedirectURL = config.DefaultRedirectURL
	}

	rc.ResolvedDeviceName = cfg.DeviceName
	if rc.ResolvedDeviceName == "" && cfg.EnableLibrespot {
		rc.ResolvedDeviceName = librespot.DefaultDeviceName
	}

	return rc
}
