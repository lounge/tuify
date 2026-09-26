package librespot

import (
	"os/exec"
	"syscall"
)

// bindToParent has the kernel send librespot SIGTERM when tuify dies, even
// by SIGKILL or a crash that skips Stop, so it can't linger as an orphaned
// Connect device. Pdeathsig fires when the creating OS thread exits; the Go
// runtime keeps its threads for the life of the process unless a goroutine
// exits while locked to one, which this program doesn't do.
func bindToParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
