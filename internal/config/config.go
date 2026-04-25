package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lounge/tuify/internal/theme"
)

const DefaultRedirectURL = "http://127.0.0.1:4444/callback"

type Config struct {
	ClientID        string      `json:"client_id"`
	EnableLibrespot bool        `json:"enable_librespot,omitempty"`
	LibrespotPath   string      `json:"librespot_path,omitempty"`
	DeviceName      string      `json:"device_name,omitempty"`
	Bitrate         int         `json:"bitrate,omitempty"`
	SpotifyUsername string      `json:"spotify_username,omitempty"`
	RedirectURL     string      `json:"redirect_url,omitempty"`
	AudioBackend    string      `json:"audio_backend,omitempty"`
	VimMode         bool        `json:"vim_mode,omitempty"`
	// Appearance forces dark or light palette selection. Empty (omitted)
	// uses lipgloss's terminal-background autodetection. Valid: "", "dark",
	// "light".
	Appearance string      `json:"appearance,omitempty"`
	Theme      theme.Theme `json:"theme,omitempty"`
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
	switch c.Appearance {
	case "", "dark", "light":
	default:
		return fmt.Errorf(`invalid appearance %q: must be "dark", "light", or empty for auto`, c.Appearance)
	}
	if err := theme.Validate(c.Theme); err != nil {
		return err
	}
	return nil
}

func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	// DisallowUnknownFields surfaces typo'd keys as errors instead of
	// silently dropping them. Without it, a mistyped field name (a missing
	// letter, swapped order, etc.) would leave the user puzzling over why
	// their setting "doesn't work" while the value never reached the code.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
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
