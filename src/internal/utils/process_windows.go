//go:build windows

package utils

import (
	"os"
	"os/exec"
)

// TerminateProcess stops a process.
func TerminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// StopProcessGroupOnCancel keeps exec's own cancel, which kills the process.
func StopProcessGroupOnCancel(_ *exec.Cmd) {}
