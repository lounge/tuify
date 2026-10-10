package bootstrap

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lounge/tuify/internal/config"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/theme"
)

// --- loadOrSetupConfig tests ---

func TestLoadOrSetupConfig_ExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Pre-create a config file.
	cfg := &config.Config{ClientID: "existing-id"}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := loadOrSetupConfig(nil, nil)
	if err != nil {
		t.Fatalf("loadOrSetupConfig: %v", err)
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

	got, err := loadOrSetupConfig(input, &output)
	if err != nil {
		t.Fatalf("loadOrSetupConfig: %v", err)
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

	_, err := loadOrSetupConfig(input, &output)
	if err == nil {
		t.Fatal("expected error for empty client ID")
	}
	if !strings.Contains(err.Error(), "client ID") {
		t.Errorf("error should mention client ID, got: %v", err)
	}
}

// With stdin closed or not a terminal there is no answer to read. The
// error must say how to get a config instead of claiming the user left
// the client ID empty.
func TestLoadOrSetupConfig_NoInput(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	_, err := loadOrSetupConfig(strings.NewReader(""), &strings.Builder{})
	if err == nil {
		t.Fatal("expected an error with no input")
	}
	want := filepath.Join(tmp, "tuify", "config.json")
	if !strings.Contains(err.Error(), "run tuify in a terminal") || !strings.Contains(err.Error(), want) {
		t.Errorf("error should explain how to create %s, got: %v", want, err)
	}
}

func TestSetupLog_KeepsPreviousRunAndIsPrivate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "tuify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte("crash details"), 0o644); err != nil { //nolint:gosec // the old default mode, to check it gets tightened
		t.Fatal(err)
	}
	prev := log.Writer()
	t.Cleanup(func() { log.SetOutput(prev) })

	closeLog := setupLog()
	closeLog()

	old, err := os.ReadFile(filepath.Join(dir, "debug.log.1"))
	if err != nil || string(old) != "crash details" {
		t.Errorf("previous log not kept as debug.log.1: %q, %v", old, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for _, name := range []string{"debug.log", "debug.log.1"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s permissions = %o, want 600", name, perm)
		}
	}
}

func TestLoadOrSetupConfig_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("bad json"), 0o600)

	_, err := loadOrSetupConfig(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- resolveRuntime tests ---

func TestResolveRuntime_Defaults(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
	}
	rc := resolveRuntime(cfg)

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
	rc := resolveRuntime(cfg)

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
	rc := resolveRuntime(cfg)

	// DeviceName should be empty when librespot is disabled.
	if rc.ResolvedDeviceName != "" {
		t.Errorf("DeviceName: got %q, want empty", rc.ResolvedDeviceName)
	}
}

// --- startLibrespot tests ---

func TestStartLibrespot_Disabled(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: false,
	}
	rc := resolveRuntime(cfg)
	client := &spotify.Client{}

	svc, err := startLibrespot(context.Background(), rc, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc != nil {
		t.Error("expected nil when librespot is disabled")
	}
}

// TestNewSpotifyClient_PreferredDevice: tuify's librespot device is the
// preferred one only when librespot is enabled.
func TestNewSpotifyClient_PreferredDevice(t *testing.T) {
	for _, tc := range []struct {
		enable bool
		want   string
	}{{true, "test-device"}, {false, ""}} {
		rc := resolveRuntime(&config.Config{ClientID: "id", EnableLibrespot: tc.enable, DeviceName: "test-device"})
		client := newSpotifyClient(&http.Client{}, rc)
		if got := client.PreferredDevice(); got != tc.want {
			t.Errorf("librespot enabled=%v: PreferredDevice() = %q, want %q", tc.enable, got, tc.want)
		}
	}
}

// TestStartLibrespot_ReturnsOptions requires a working binary; skip if none
// is available. When the binary runs, we should receive at least the audio
// source and inactive-channel options.
func TestStartLibrespot_ReturnsOptions(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
		LibrespotPath:   "/bin/true",
	}
	rc := resolveRuntime(cfg)
	client := &spotify.Client{}

	svc, err := startLibrespot(context.Background(), rc, client)
	if err != nil {
		t.Skipf("librespot binary unavailable: %v", err)
	}
	defer svc.Cleanup()

	if len(svc.Options) < 2 {
		t.Errorf("expected at least 2 UI model options (audio source + inactive channel), got %d", len(svc.Options))
	}
}

func TestStartLibrespot_ErrorOnBinaryMissing(t *testing.T) {
	cfg := &config.Config{
		ClientID:        "id",
		EnableLibrespot: true,
		LibrespotPath:   "/no/such/binary-that-definitely-does-not-exist",
	}
	rc := resolveRuntime(cfg)
	client := &spotify.Client{}

	svc, err := startLibrespot(context.Background(), rc, client)
	if err == nil {
		if svc != nil {
			svc.Cleanup()
		}
		t.Fatal("expected error when librespot binary doesn't exist")
	}
	if svc != nil {
		t.Error("expected nil services on error")
	}
}

// --- backfillThemeDefaults tests ---

func TestBackfillThemeDefaults_FillsEmptyThemeAndPersists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Pre-theme-feature config: client_id only, no theme block.
	dir := filepath.Join(tmp, "tuify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	pre := []byte(`{"client_id":"abc","enable_librespot":true}`)
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, pre, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := loadOrSetupConfig(nil, nil)
	if err != nil {
		t.Fatalf("loadOrSetupConfig: %v", err)
	}

	// In-memory cfg must have the defaults populated.
	want := theme.Default()
	if got.Theme != want {
		t.Errorf("in-memory theme not populated\n got: %+v\nwant: %+v", got.Theme, want)
	}
	if got.ClientID != "abc" {
		t.Errorf("ClientID lost: got %q", got.ClientID)
	}
	if !got.EnableLibrespot {
		t.Error("EnableLibrespot lost")
	}

	// File on disk must now contain the theme block. Reload through Load
	// to verify the persisted form parses cleanly with DisallowUnknownFields.
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload after backfill: %v", err)
	}
	if loaded.Theme != want {
		t.Errorf("persisted theme mismatch\n got: %+v\nwant: %+v", loaded.Theme, want)
	}
}

func TestBackfillThemeDefaults_LeavesPartialThemeAlone(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// User overrode just primary.dark — must not be overwritten by backfill.
	pre := []byte(`{"client_id":"abc","theme":{"primary":{"dark":"#ff0000"}}}`)
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, pre, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := loadOrSetupConfig(nil, nil)
	if err != nil {
		t.Fatalf("loadOrSetupConfig: %v", err)
	}

	if got.Theme.Primary.Dark != "#ff0000" {
		t.Errorf("user override clobbered: got %+v", got.Theme.Primary)
	}
	// Other roles stay zero-valued — Apply will fall back to package
	// defaults at startup. Filling them here would erase the distinction
	// between "user provided some keys" and "first launch".
	if got.Theme.Secondary != (theme.Variant{}) {
		t.Errorf("partial theme over-filled: Secondary = %+v", got.Theme.Secondary)
	}
}

// An invalid config must be reported without being rewritten: the theme
// backfill used to save it (with the defaults added) before Validate ran,
// so the user's file changed under them on a launch that then failed.
func TestLoadOrSetupConfig_InvalidConfigIsNotRewritten(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "tuify")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// No theme block, so a backfill would want to write.
	original := []byte(`{"client_id":"abc","bitrate":128}`)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadOrSetupConfig(nil, nil)
	if err == nil {
		t.Fatal("loadOrSetupConfig accepted bitrate 128")
	}
	if !strings.Contains(err.Error(), "128") || !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the bad value and %s, got: %v", path, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("invalid config was rewritten:\n got: %s\nwant: %s", got, original)
	}
}
