package config

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

const DefaultRedirectURL = "http://127.0.0.1:4444/callback"

type Config struct {
	ClientID        string `json:"client_id"`
	EnableLibrespot bool   `json:"enable_librespot,omitempty"`
	LibrespotPath   string `json:"librespot_path,omitempty"`
	DeviceName      string `json:"device_name,omitempty"`
	Bitrate         int    `json:"bitrate,omitempty"`
	SpotifyUsername string `json:"spotify_username,omitempty"`
	RedirectURL     string `json:"redirect_url,omitempty"`
	AudioBackend    string `json:"audio_backend,omitempty"`
	VimMode         bool   `json:"vim_mode,omitempty"`
}

func Dir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "tuify")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[config] failed to resolve home directory: %v", err)
	}
	return filepath.Join(home, ".config", "tuify")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
}
