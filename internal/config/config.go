package config

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"github.com/lounge/tuify/internal/theme"
)

// DefaultRedirectURL is the OAuth redirect used when the config sets none.
// It must be registered for the app in the Spotify dashboard.
const DefaultRedirectURL = "http://127.0.0.1:4444/callback"

// AudioBackends lists the audio_backend values Validate accepts: every
// backend librespot itself accepts. "pipe" is the one tuify plays through
// itself; the rest hand audio to librespot's own output, and all but
// rodio, alsa, pulseaudio and subprocess need librespot built with the
// matching cargo feature.
var AudioBackends = []string{"pipe", "rodio", "alsa", "pulseaudio", "jackaudio", "portaudio", "gstreamer", "sdl", "subprocess", "rodiojack"}

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

// Dir returns the tuify config directory. Honors $XDG_CONFIG_HOME when it
// is an absolute path (the XDG spec says a relative value is to be
// ignored, and honoring one would scatter config.json, token.json and
// debug.log under whatever directory tuify was started from), otherwise
// derives from the user's home directory. Returns an error if neither is
// available — silently defaulting to an empty path meant every downstream
// "failed to open" error pointed at a phantom file at the repo root.
func Dir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" && filepath.IsAbs(dir) {
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
	if c.AudioBackend != "" && !slices.Contains(AudioBackends, c.AudioBackend) {
		return fmt.Errorf("invalid audio_backend %q: must be one of %v", c.AudioBackend, AudioBackends)
	}
	if err := validateRedirectURL(c.RedirectURL); err != nil {
		return err
	}
	if err := theme.Validate(c.Theme); err != nil {
		return err
	}
	return nil
}

// validateRedirectURL checks a non-empty redirect_url is one the login
// flow can serve: an http URL with a host and a port, since auth.Login
// listens on exactly that host:port. The host itself is not restricted
// (Spotify's dashboard wants a loopback IP literal such as 127.0.0.1, but
// an existing config naming localhost must keep loading).
func validateRedirectURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid redirect_url %q: %w", raw, err)
	}
	switch {
	case u.Scheme != "http":
		return fmt.Errorf("invalid redirect_url %q: scheme must be http, the login callback server speaks plain HTTP", raw)
	case u.Hostname() == "":
		return fmt.Errorf("invalid redirect_url %q: missing host", raw)
	case u.Port() == "":
		return fmt.Errorf("invalid redirect_url %q: missing port (for example %s)", raw, DefaultRedirectURL)
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
// Unknown, duplicate, or wrongly cased keys and trailing content are
// errors. Load does not call Validate.
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
	// RejectUnknownMembers surfaces typo'd keys as errors instead of
	// silently dropping them. Without it, a mistyped field name (a missing
	// letter, swapped order, etc.) would leave the user puzzling over why
	// their setting "doesn't work" while the value never reached the code.
	// json/v2 adds the rest of the strictness for free: names match
	// case-sensitively (so "Client_ID" is unknown, not a lucky hit), a key
	// written twice is an error instead of last-one-wins, and anything
	// after the config object (a second object pasted in, a stray
	// fragment) is rejected rather than ignored.
	var cfg Config
	if err := json.Unmarshal(data, &cfg, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
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
	data, err := json.Marshal(cfg, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	return WriteFileAtomic(filepath.Join(dir, "config.json"), data)
}
