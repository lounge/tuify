package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// errLinkLoop is the error for a symlink chain that never reaches a file.
var errLinkLoop = errors.New("too many levels of symbolic links")

// maxSymlinkDepth bounds how many links resolveWriteTarget follows by hand
// when the final target does not exist yet, so a link loop is an error
// rather than an endless walk.
const maxSymlinkDepth = 32

// WriteFileAtomic writes data to path with 0600 permissions such that a
// reader sees either the old contents or the new, never a truncated mix.
// It writes a temp file in the same directory, syncs it and renames it
// over path. os.WriteFile truncates in place, so a crash or a second
// tuify writing at the same time could leave invalid JSON behind that
// then fails the next startup.
//
// A symlink at path is written through, not replaced: the temp file is
// created next to the link's target and renamed over that, so a
// config.json linked in from a dotfiles repository stays a link. A
// dangling link is followed the same way and its target created.
//
// The rename also replaces the file's permissions, so a file that was
// made group- or world-readable by hand is tightened back to 0600. After
// the rename the directory is synced too, so the new name survives a
// crash right after this returns.
func WriteFileAtomic(path string, data []byte) (err error) {
	target, err := resolveWriteTarget(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
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
	if err = os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// resolveWriteTarget returns the file a write to path must land on: path
// itself, or the file a symlink at path points to. When the target
// exists filepath.EvalSymlinks resolves the whole chain; when it does not
// (no file yet, or a dangling link) the final component is followed by
// hand so the link is kept and its target created.
func resolveWriteTarget(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for range maxSymlinkDepth {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			// Missing, or a real file: write here. A missing directory
			// surfaces from CreateTemp with the path in the error.
			return path, nil
		}
		dest, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(path), dest)
		}
		path = dest
	}
	return "", &fs.PathError{Op: "readlink", Path: path, Err: errLinkLoop}
}

// syncDir flushes the directory so the entry added by the rename is on
// disk. Best effort: Windows cannot sync a directory handle, and a lost
// rename only reverts to the previous, still valid, file.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
