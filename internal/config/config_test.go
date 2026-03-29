package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDir_XDGOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	got := Dir()
	want := filepath.Join(tmp, "tuify")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_DefaultHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := Dir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "tuify")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
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
	for _, field := range []string{"enable_librespot", "librespot_path", "device_name", "bitrate", "vim_mode"} {
		if strings.Contains(s, field) {
			t.Errorf("expected %q to be omitted from JSON, got: %s", field, s)
		}
	}
}
