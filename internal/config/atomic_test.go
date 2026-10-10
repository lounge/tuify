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

// A failure after the temp file exists (here: the rename, because the
// target is a directory) must not leave the temp file behind.
func TestWriteFileAtomic_FailureRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(target, []byte("x")); err == nil {
		t.Fatal("expected an error renaming over a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only config.json (temp file left behind?)", names)
	}
}

// symlink creates a link or skips the test where links need privileges
// the test runner may lack (Windows).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

// A config.json that is a symlink (a dotfiles repository linked into
// place) must be written through: the link stays a link and the file it
// points to gets the new contents.
func TestWriteFileAtomic_WritesThroughSymlink(t *testing.T) {
	repo := t.TempDir()
	cfgDir := t.TempDir()
	real := filepath.Join(repo, "config.json")
	link := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(real, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink(t, real, link)

	if err := WriteFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if got, err := os.ReadFile(real); err != nil || string(got) != "new" {
		t.Errorf("link target = %q, %v; want new", got, err)
	}
	for _, dir := range []string{repo, cfgDir} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("%s holds %d entries, want 1 (temp file left behind?)", dir, len(entries))
		}
	}
}

// A dangling link (the dotfiles repository has no config.json yet) is
// followed too, so the first write creates the target instead of
// replacing the link.
func TestWriteFileAtomic_CreatesDanglingSymlinkTarget(t *testing.T) {
	repo := t.TempDir()
	cfgDir := t.TempDir()
	real := filepath.Join(repo, "config.json")
	link := filepath.Join(cfgDir, "config.json")
	// Relative link, as a dotfiles manager would create it.
	rel, err := filepath.Rel(cfgDir, real)
	if err != nil {
		t.Fatal(err)
	}
	symlink(t, rel, link)

	if err := WriteFileAtomic(link, []byte("first")); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link = %v, %v; want it kept as a symlink", info, err)
	}
	if got, err := os.ReadFile(real); err != nil || string(got) != "first" {
		t.Errorf("link target = %q, %v; want first", got, err)
	}
}

func TestWriteFileAtomic_SymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	symlink(t, "b", a)
	symlink(t, "a", b)

	if err := WriteFileAtomic(a, []byte("x")); err == nil {
		t.Fatal("expected an error for a symlink loop")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries, want the two links only", len(entries))
	}
}
