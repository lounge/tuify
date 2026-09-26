//go:build !linux

package librespot

import "os/exec"

// bindToParent is a no-op where the OS has no parent-death signal
// (macOS, Windows). There, Stop on every normal exit path, including
// SIGHUP/SIGTERM handled by bootstrap, is what stops librespot.
func bindToParent(*exec.Cmd) {}
