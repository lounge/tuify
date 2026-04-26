package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/theme"
)

func TestDir_XDGOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(tmp, "tuify")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_DefaultHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "tuify")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_HomeLookupFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	// os.UserHomeDir consults HOME on Unix and USERPROFILE on Windows.
	// Clear both so the lookup fails deterministically and we can assert
	// the error is propagated instead of silently returning an empty path.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := Dir(); err == nil {
		t.Fatal("expected error when home cannot be resolved")
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &Config{
		ClientID:        "test-id",
		EnableLibrespot: true,
		LibrespotPath:   "/usr/bin/librespot",
		DeviceName:      "mydevice",
		Bitrate:         160,
		SpotifyUsername: "user1",
		RedirectURL:     "http://localhost:8888/callback",
		AudioBackend:    "pulseaudio",
		VimMode:         true,
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if *loaded != *cfg {
		t.Errorf("Load() = %+v, want %+v", loaded, cfg)
	}
}

func TestLoad_NoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing config, got %+v", cfg)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("not json"), 0o600)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidate_ValidBitrates(t *testing.T) {
	for _, br := range []int{0, 96, 160, 320} {
		cfg := &Config{ClientID: "id", Bitrate: br}
		if err := cfg.Validate(); err != nil {
			t.Errorf("bitrate %d should be valid, got: %v", br, err)
		}
	}
}

func TestValidate_InvalidBitrate(t *testing.T) {
	cfg := &Config{ClientID: "id", Bitrate: 128}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for bitrate 128")
	}
	if !strings.Contains(err.Error(), "128") {
		t.Errorf("error should mention invalid value, got: %v", err)
	}
}

func TestValidate_MissingClientID(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error should mention client_id, got: %v", err)
	}
}

func TestSave_OmitsEmptyFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &Config{ClientID: "only-id"}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "tuify", "config.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// omitempty fields should not appear
	s := string(data)
	for _, field := range []string{"enable_librespot", "librespot_path", "device_name", "bitrate", "vim_mode", "appearance", "theme"} {
		if strings.Contains(s, field) {
			t.Errorf("expected %q to be omitted from JSON, got: %s", field, s)
		}
	}
}

func TestValidate_AppearanceValues(t *testing.T) {
	cases := []struct {
		appearance string
		ok         bool
	}{
		{"", true},
		{"dark", true},
		{"light", true},
		{"Dark", false}, // case-sensitive
		{"system", false},
		{"auto", false},
		{"invalid", false},
	}
	for _, tc := range cases {
		t.Run(tc.appearance, func(t *testing.T) {
			cfg := &Config{ClientID: "id", Appearance: tc.appearance}
			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Errorf("Validate(%q): unexpected error: %v", tc.appearance, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("Validate(%q): expected error, got nil", tc.appearance)
				}
				if !strings.Contains(err.Error(), "appearance") {
					t.Errorf("error should mention 'appearance', got: %v", err)
				}
				if !strings.Contains(err.Error(), tc.appearance) {
					t.Errorf("error should quote the bad value %q, got: %v", tc.appearance, err)
				}
			}
		})
	}
}

func TestValidate_RejectsBadTheme(t *testing.T) {
	cfg := &Config{
		ClientID: "id",
		Theme: theme.Theme{
			Primary: theme.Variant{Light: "not-a-color"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid theme color")
	}
	// Validate should delegate to theme.Validate, surfacing the JSON path.
	if !strings.Contains(err.Error(), "theme.primary.light") {
		t.Errorf("error should name JSON path, got: %v", err)
	}
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "tuify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// "vim_mod" mimics a realistic user typo (truncated "vim_mode") —
	// silently ignored without DisallowUnknownFields. With it, Load must
	// surface the unknown key.
	bad := []byte(`{"client_id":"abc","vim_mod":true}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), bad, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "vim_mod") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
	// Path of the offending file should be wrapped in for clarity.
	if !strings.Contains(err.Error(), "config.json") {
		t.Errorf("error should include the file path, got: %v", err)
	}
}
