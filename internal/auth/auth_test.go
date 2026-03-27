package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSaveAndLoadToken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	token := &oauth2.Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		Expiry:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := SaveToken(token); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	// Verify file exists with correct permissions
	path := filepath.Join(tmp, "tuify", "token.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("token file not found: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file permissions: got %o, want 600", perm)
	}

	loaded, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadToken returned nil")
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}

	// Verify raw file is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved token is not valid JSON: %v", err)
	}
	if parsed["access_token"] != "access-123" {
		t.Errorf("raw JSON access_token: got %v", parsed["access_token"])
	}
}

func TestLoadToken_NoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	token, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if token != nil {
		t.Errorf("expected nil for missing token, got %+v", token)
	}
}

func TestLoadToken_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "tuify")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token.json"), []byte("not json"), 0o600)

	_, err := LoadToken()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGenerateRandomBase64(t *testing.T) {
	a := generateRandomBase64(32)
	b := generateRandomBase64(32)

	if a == b {
		t.Error("two random values should not be equal")
	}
	// 32 bytes -> 43 chars in base64 raw URL encoding
	if len(a) != 43 {
		t.Errorf("expected length 43, got %d", len(a))
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test-verifier-string"
	c1 := generateCodeChallenge(verifier)
	c2 := generateCodeChallenge(verifier)

	if c1 != c2 {
		t.Error("same verifier should produce same challenge")
	}
	if len(c1) == 0 {
		t.Error("challenge should not be empty")
	}
	if c1 == verifier {
		t.Error("challenge should differ from verifier")
	}
}
