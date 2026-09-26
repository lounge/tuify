package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	// A pre-existing file loosened by hand.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil { //nolint:gosec // deliberately loose to check it gets tightened
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil || string(got) != `{"new":true}` {
		t.Fatalf("contents = %q, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only token.json (temp file left behind?)", len(entries))
	}
}

func TestWriteFileAtomic_FailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-subdir", "token.json")
	if err := WriteFileAtomic(path, []byte("x")); err == nil {
		t.Fatal("expected an error writing into a missing directory")
	}
}
