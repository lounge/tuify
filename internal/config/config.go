package config

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Dir returns the tuify config directory. Honors $XDG_CONFIG_HOME, otherwise
// derives from the user's home directory. Returns an error if neither is
// available — silently defaulting to an empty path meant every downstream
// "failed to open" error pointed at a phantom file at the repo root.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "tuify"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "tuify"), nil
}

// Validate checks that configured values are valid. Zero values (omitted
// fields) are not checked — defaults are applied elsewhere.
func (c *Config) Validate() error {
	if c.Bitrate != 0 && c.Bitrate != 96 && c.Bitrate != 160 && c.Bitrate != 320 {
		return fmt.Errorf("invalid bitrate %d: must be 96, 160, or 320", c.Bitrate)
	}
	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	return nil
}

func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
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
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
}
