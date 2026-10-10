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

// DefaultRedirectURL is the OAuth redirect used when the config sets none.
// It must be registered for the app in the Spotify dashboard.
const DefaultRedirectURL = "http://127.0.0.1:4444/callback"

// Config mirrors config.json. Omitted fields keep their zero value and
// get defaults at runtime (bootstrap.resolveRuntime, librespot.Config).
type Config struct {
	ClientID        string `json:"client_id"`
	EnableLibrespot bool   `json:"enable_librespot,omitzero"`
	LibrespotPath   string `json:"librespot_path,omitempty"`
	DeviceName      string `json:"device_name,omitempty"`
	Bitrate         int    `json:"bitrate,omitzero"`
	SpotifyUsername string `json:"spotify_username,omitempty"`
	RedirectURL     string `json:"redirect_url,omitempty"`
	AudioBackend    string `json:"audio_backend,omitempty"`
	VimMode         bool   `json:"vim_mode,omitzero"`
	// NerdFont selects Nerd Font glyphs (e.g. the shuffle icon). Nil
	// (omitted) auto-detects an installed Nerd Font; true/false forces it.
	NerdFont *bool `json:"nerd_font,omitzero"`
	// Appearance forces dark or light palette selection. Empty (omitted)
	// uses lipgloss's terminal-background autodetection. Valid: "", "dark",
	// "light".
	Appearance string      `json:"appearance,omitempty"`
	Theme      theme.Theme `json:"theme,omitzero"`
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

// Path returns the location of config.json inside Dir.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads and parses config.json. It returns (nil, nil) when the file
// doesn't exist yet, which callers treat as "run first-time setup".
// Unknown keys are an error. Load does not call Validate.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
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
	// Decode stops after the first JSON value. Anything after it (a
	// second object pasted in, a stray fragment) would be ignored
	// silently, unknown keys included.
	if dec.More() {
		return nil, fmt.Errorf("parse %s: unexpected content after the config object", path)
	}
	return &cfg, nil
}

// Save writes cfg to config.json with WriteFileAtomic, creating the config
// directory (0700) if needed.
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
	return WriteFileAtomic(filepath.Join(dir, "config.json"), data)
}
