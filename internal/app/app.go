package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/audio"
	"github.com/lounge/tuify/internal/auth"
	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/librespot"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/ui"
	sp "github.com/zmb3/spotify/v2"
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
// directory. Returns a cleanup function that closes the log file.
func SetupLog() func() {
	logPath := filepath.Join(config.Dir(), "debug.log")
	if err := os.MkdirAll(config.Dir(), 0o700); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
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

// AuthSession holds the result of auth bootstrap.
type AuthSession struct {
	Client *spotify.Client
}

// Bootstrap authenticates with Spotify and returns a ready-to-use session.
// If no saved token exists, it runs the interactive login flow.
func Bootstrap(rc RuntimeConfig) (*AuthSession, error) {
	token, err := auth.LoadToken()
	if err != nil {
		return nil, fmt.Errorf("loading token: %w", err)
	}

	authenticator := auth.NewAuthenticator(rc.ClientID, rc.ResolvedRedirectURL)

	if token == nil {
		fmt.Fprintln(os.Stderr, "No saved session found. Starting login...")
		token, err = auth.Login(authenticator, rc.ResolvedRedirectURL)
		if err != nil {
			return nil, fmt.Errorf("login failed: %w", err)
		}
		if err := auth.SaveToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save token: %v\n", err)
		}
	}

	httpClient, err := auth.NewSavingClient(authenticator, token)
	if err != nil {
		return nil, err
	}

	spClient := sp.New(httpClient)
	client := spotify.New(spClient, httpClient)
	if err := client.FetchUserID(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch user ID: %v\n", err)
	}

	return &AuthSession{Client: client}, nil
}

// LibrespotServices holds the result of librespot/audio startup.
type LibrespotServices struct {
	Options []ui.ModelOption
	Cleanup func()
}

// StartLibrespot starts the librespot process and audio receiver if enabled
// by the config. Returns UI model options and a cleanup function. If librespot
// is not enabled, returns nil.
func StartLibrespot(rc RuntimeConfig, client *spotify.Client) *LibrespotServices {
	if !rc.EnableLibrespot {
		return nil
	}

	client.PreferredDevice = rc.ResolvedDeviceName

	backend := rc.AudioBackend
	if backend == "" {
		backend = librespot.DefaultBackend
	}

	lsCfg := librespot.Config{
		BinaryPath: rc.LibrespotPath,
		DeviceName: rc.ResolvedDeviceName,
		Bitrate:    rc.Bitrate,
		Backend:    backend,
		Username:   rc.SpotifyUsername,
		CacheDir:   filepath.Join(config.Dir(), "librespot"),
	}

	var cleanups []func()
	var opts []ui.ModelOption

	if backend == librespot.DefaultBackend {
		audioRecv := audio.NewReceiver()
		if err := audioRecv.Start(); err != nil {
			log.Printf("[startup] audio receiver failed: %v", err)
			return &LibrespotServices{Cleanup: func() {}}
		}
		cleanups = append(cleanups, audioRecv.Stop)
		opts = append(opts, ui.WithAudioReceiver(audioRecv))

		selfPath, err := os.Executable()
		if err != nil {
			selfPath = os.Args[0]
		}
		selfPath = filepath.ToSlash(selfPath)
		log.Printf("[librespot] audio worker command: %s --audio-worker --socket %s", selfPath, audioRecv.SocketPath())
		lsCfg.AudioWorker = fmt.Sprintf("%s --audio-worker --socket %s", selfPath, audioRecv.SocketPath())
	}

	librespotProc := librespot.NewProcess(lsCfg)
	librespotProc.OnReconnect = reconnectHandler(client, rc.ResolvedDeviceName)

	if err := librespotProc.Start(); err != nil {
		log.Printf("[startup] librespot failed to start: %v", err)
	} else {
		cleanups = append(cleanups, func() { librespotProc.Stop() })
	}

	inactiveCh := make(chan struct{}, 1)
	librespotProc.OnInactive = func() {
		select {
		case inactiveCh <- struct{}{}:
		default:
		}
	}
	opts = append(opts, ui.WithLibrespotInactive(inactiveCh))

	return &LibrespotServices{
		Options: opts,
		Cleanup: func() {
			// Cleanup in reverse order (librespot before audio receiver).
			for i := len(cleanups) - 1; i >= 0; i-- {
				cleanups[i]()
			}
		},
	}
}

// Run is the main application entry point. It loads config, authenticates,
// starts services, and runs the TUI. Returns an error on startup or runtime
// failure.
func Run() error {
	closeLog := SetupLog()
	defer closeLog()

	cfg, err := LoadOrSetupConfig(nil, nil)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	rc := ResolveRuntime(cfg)

	session, err := Bootstrap(rc)
	if err != nil {
		return err
	}

	var opts []ui.ModelOption
	if cfg.VimMode {
		opts = append(opts, ui.WithVimMode())
	}

	if svc := StartLibrespot(rc, session.Client); svc != nil {
		defer svc.Cleanup()
		opts = append(opts, svc.Options...)
	}

	p := tea.NewProgram(ui.NewModel(session.Client, opts...), tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// reconnectHandler returns a callback for librespot reconnection that
// transfers playback back to the preferred device (unless overridden).
func reconnectHandler(client *spotify.Client, deviceName string) func() {
	return func() {
		time.Sleep(2 * time.Second)
		if client.DeviceOverridden.Load() {
			log.Printf("[librespot] reconnect: device was manually switched, skipping transfer")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		devID, _, _, err := client.FindDevice(ctx, false)
		if err != nil {
			log.Printf("[librespot] reconnect: could not find device: %v", err)
			return
		}
		if err := client.TransferPlayback(ctx, devID, true); err != nil {
			log.Printf("[librespot] reconnect: transfer playback failed: %v", err)
		} else {
			log.Printf("[librespot] reconnect: playback transferred to %s", deviceName)
		}
	}
}
