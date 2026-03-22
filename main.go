package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/auth"
	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/ui"
	sp "github.com/zmb3/spotify/v2"
)

func main() {
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

	authenticator := auth.NewAuthenticator(cfg.ClientID)

	if token == nil {
		fmt.Println("No saved session found. Starting login...")
		token, err = auth.Login(authenticator)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			os.Exit(1)
		}
		if err := auth.SaveToken(token); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save token: %v\n", err)
		}
	}

	httpClient := auth.NewSavingClient(authenticator, token)
	spClient := sp.New(httpClient)
	client := spotify.New(spClient, httpClient)
	if err := client.FetchUserID(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch user ID: %v\n", err)
	}

	p := tea.NewProgram(ui.NewModel(client), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
