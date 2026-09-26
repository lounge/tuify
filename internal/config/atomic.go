package config

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path with 0600 permissions such that a
// reader sees either the old contents or the new, never a truncated mix.
// It writes a temp file in the same directory, syncs it and renames it
// over path. os.WriteFile truncates in place, so a crash or a second
// tuify writing at the same time could leave invalid JSON behind that
// then fails the next startup.
//
// The rename also replaces the file's permissions, so a file that was
// made group- or world-readable by hand is tightened back to 0600.
func WriteFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
