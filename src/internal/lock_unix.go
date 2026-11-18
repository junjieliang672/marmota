//go:build !windows

package internal

import (
	"os"
	"syscall"
)

// lockFile takes the lock on the file at path, made when it isn't there,
// waiting for a program that holds it; the function it returns lets it go.
// The system lets it go too when this program ends. With no file there is
// no lock.
func lockFile(path string) func() {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
