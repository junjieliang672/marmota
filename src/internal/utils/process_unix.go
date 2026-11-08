//go:build !windows

package utils

import (
	"os/exec"
	"syscall"
)

// TerminateProcess asks a process to exit.
func TerminateProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

// StopProcessGroupOnCancel starts cmd in a process group of its own and
// makes cancelling its context stop the whole group, so commands it started
// stop with it.
func StopProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
