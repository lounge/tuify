package main

import (
	"bufio"
	"context"
	"fmt"
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

func main() {
	// Audio worker subcommand: librespot pipes PCM to stdin, we play + FFT.
	if len(os.Args) > 1 && os.Args[1] == "--audio-worker" {
		runAudioWorker(os.Args[2:])
		return
	}

	logPath := filepath.Join(config.Dir(), "debug.log")
	if err := os.MkdirAll(config.Dir(), 0o700); err == nil {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg == nil {
		cfg = runSetup()
	}

	token, err := auth.LoadToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading token: %v\n", err)
		os.Exit(1)
	}

	redirectURL := cfg.RedirectURL
	if redirectURL == "" {
		redirectURL = config.DefaultRedirectURL
	}
	authenticator := auth.NewAuthenticator(cfg.ClientID, redirectURL)

	if token == nil {
		fmt.Println("No saved session found. Starting login...")
		token, err = auth.Login(authenticator, redirectURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			os.Exit(1)
		}
		if err := auth.SaveToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save token: %v\n", err)
		}
	}

	httpClient, err := auth.NewSavingClient(authenticator, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	spClient := sp.New(httpClient)
	client := spotify.New(spClient, httpClient)
	if err := client.FetchUserID(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch user ID: %v\n", err)
	}

	var opts []ui.ModelOption

	if cfg.VimMode {
		opts = append(opts, ui.WithVimMode())
	}

	// Start librespot + audio receiver if enabled.
	if cfg.EnableLibrespot {
		deviceName := cfg.DeviceName
		if deviceName == "" {
			deviceName = librespot.DefaultDeviceName
		}
		client.PreferredDevice = deviceName

		backend := cfg.AudioBackend
		if backend == "" {
			backend = librespot.DefaultBackend
		}

		lsCfg := librespot.Config{
			BinaryPath: cfg.LibrespotPath,
			DeviceName: deviceName,
			Bitrate:    cfg.Bitrate,
			Backend:    backend,
			Username:   cfg.SpotifyUsername,
			CacheDir:   filepath.Join(config.Dir(), "librespot"),
		}

		startOK := true
		if backend == librespot.DefaultBackend {
			audioRecv := audio.NewReceiver()
			if err := audioRecv.Start(); err != nil {
				log.Printf("[startup] audio receiver failed: %v", err)
				startOK = false
			} else {
				defer audioRecv.Stop()
				opts = append(opts, ui.WithAudioReceiver(audioRecv))

				selfPath, err := os.Executable()
				if err != nil {
					selfPath = os.Args[0]
				}
				// librespot subprocess backend uses shell_words::split (POSIX rules)
				// which treats backslashes as escape chars — use forward slashes on Windows.
				selfPath = filepath.ToSlash(selfPath)
				log.Printf("[librespot] audio worker command: %s --audio-worker --socket %s", selfPath, audioRecv.SocketPath())
				lsCfg.AudioWorker = fmt.Sprintf("%s --audio-worker --socket %s", selfPath, audioRecv.SocketPath())
			}
		}

		if startOK {
			librespotProc := librespot.NewProcess(lsCfg)
			librespotProc.OnReconnect = func() {
				time.Sleep(2 * time.Second)
				if client.DeviceOverridden.Load() {
					log.Printf("[librespot] reconnect: device was manually switched, skipping transfer")
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				devID, _, err := client.FindDevice(ctx, false)
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
			if err := librespotProc.Start(); err != nil {
				log.Printf("[startup] librespot failed to start: %v", err)
			} else {
				defer librespotProc.Stop()
			}
		}
	}

	p := tea.NewProgram(ui.NewModel(client, opts...), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAudioWorker(args []string) {
	// Log to stderr — librespot captures this and it flows to the main process log.
	fmt.Fprintf(os.Stderr, "[audio-worker] started with args: %v\n", args)

	var socketPath string
	for i, a := range args {
		if a == "--socket" && i+1 < len(args) {
			socketPath = args[i+1]
		}
	}
	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "[audio-worker] error: --socket <path> required")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[audio-worker] connecting to socket: %s\n", socketPath)

	w := &audio.Worker{
		Format:     audio.DefaultFormat,
		SocketPath: socketPath,
	}
	if err := w.Run(context.Background(), os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "[audio-worker] error: %v\n", err)
		os.Exit(1)
	}
}

func runSetup() *config.Config {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Welcome to tuify! Let's set up Spotify.")
	fmt.Println()
	fmt.Println("1. Go to https://developer.spotify.com/dashboard")
	fmt.Println("2. Create an app with redirect URI: http://127.0.0.1:4444/callback")
	fmt.Println("3. Copy your Client ID")
	fmt.Println()
	fmt.Print("Enter your Client ID: ")

	clientID, _ := reader.ReadString('\n')
	clientID = strings.TrimSpace(clientID)

	if clientID == "" {
		fmt.Fprintln(os.Stderr, "Client ID is required")
		os.Exit(1)
	}

	cfg := &config.Config{ClientID: clientID}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Config saved!")
	fmt.Println()
	return cfg
}
