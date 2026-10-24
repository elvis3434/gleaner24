//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package lock

import (
	"errors"
	"os"
	"syscall"
)

// flock locks belong to the open file description, so a second Acquire
// conflicts even within the same process, as it does across processes.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrLocked
	}
	return err
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
