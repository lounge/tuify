package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/spotify"
)

// --- LoadOrSetupConfig tests ---

func TestLoadOrSetupConfig_ExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Pre-create a config file.
	cfg := &config.Config{ClientID: "existing-id"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadOrSetupConfig(nil, nil)
	if err != nil {
		t.Fatalf("LoadOrSetupConfig: %v", err)
	}
	if got.ClientID != "existing-id" {
		t.Errorf("ClientID: got %q, want %q", got.ClientID, "existing-id")
	}
}

func TestLoadOrSetupConfig_TriggersSetup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Simulate user typing a client ID.
	input := strings.NewReader("my-new-id\n")
	var output strings.Builder

	got, err := LoadOrSetupConfig(input, &output)
	if err != nil {
		t.Fatalf("LoadOrSetupConfig: %v", err)
	}
	if got.ClientID != "my-new-id" {
		t.Errorf("ClientID: got %q, want %q", got.ClientID, "my-new-id")
	}

	// Verify setup prompt was shown.
	if !strings.Contains(output.String(), "Welcome to tuify") {
		t.Error("expected welcome message in output")
	}

	// Verify config was persisted.
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.ClientID != "my-new-id" {
		t.Errorf("persisted config: got %v", loaded)
	}
}

func TestLoadOrSetupConfig_EmptyInput(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	input := strings.NewReader("\n")
	var output strings.Builder

	_, err := LoadOrSetupConfig(input, &output)
	if err == nil {
		t.Fatal("expected error for empty client ID")
	}
	if !strings.Contains(err.Error(), "client ID") {
		t.Errorf("error should mention client ID, got: %v", err)
	}
}

func TestLoadOrSetupConfig_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("bad json"), 0o600)

	_, err := LoadOrSetupConfig(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- ResolveRuntime tests ---

func TestResolveRuntime_Defaults(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
	}
	rc := ResolveRuntime(cfg)

	if rc.ResolvedRedirectURL != config.DefaultRedirectURL {
		t.Errorf("RedirectURL: got %q, want %q", rc.ResolvedRedirectURL, config.DefaultRedirectURL)
	}
	if rc.ResolvedDeviceName != "tuify" { // librespot.DefaultDeviceName
		t.Errorf("DeviceName: got %q, want %q", rc.ResolvedDeviceName, "tuify")
	}
}

func TestResolveRuntime_CustomValues(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
		RedirectURL:     "http://custom:9999/cb",
		DeviceName:      "my-speaker",
	}
	rc := ResolveRuntime(cfg)

	if rc.ResolvedRedirectURL != "http://custom:9999/cb" {
		t.Errorf("RedirectURL: got %q", rc.ResolvedRedirectURL)
	}
	if rc.ResolvedDeviceName != "my-speaker" {
		t.Errorf("DeviceName: got %q", rc.ResolvedDeviceName)
	}
}

func TestResolveRuntime_NoLibrespot(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: false,
	}
	rc := ResolveRuntime(cfg)

	// DeviceName should be empty when librespot is disabled.
	if rc.ResolvedDeviceName != "" {
		t.Errorf("DeviceName: got %q, want empty", rc.ResolvedDeviceName)
	}
}

// --- StartLibrespot tests ---

func TestStartLibrespot_Disabled(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: false,
	}
	rc := ResolveRuntime(cfg)
	client := &spotify.Client{}

	svc := StartLibrespot(rc, client)
	if svc != nil {
		t.Error("expected nil when librespot is disabled")
	}
}

func TestStartLibrespot_SetsPreferredDevice(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
		DeviceName:      "test-device",
	}
	rc := ResolveRuntime(cfg)
	client := &spotify.Client{}

	svc := StartLibrespot(rc, client)
	if svc == nil {
		t.Fatal("expected non-nil services")
	}
	defer svc.Cleanup()

	if client.PreferredDevice != "test-device" {
		t.Errorf("PreferredDevice: got %q, want %q", client.PreferredDevice, "test-device")
	}
}

func TestStartLibrespot_ReturnsOptions(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
	}
	rc := ResolveRuntime(cfg)
	client := &spotify.Client{}

	svc := StartLibrespot(rc, client)
	if svc == nil {
		t.Fatal("expected non-nil services")
	}
	defer svc.Cleanup()

	// Should have at least the audio source option + librespot inactive channel option.
	if len(svc.Options) < 2 {
		t.Errorf("expected at least 2 UI model options (audio source + inactive channel), got %d", len(svc.Options))
	}
}
