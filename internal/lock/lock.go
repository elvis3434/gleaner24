// Package lock keeps two runs of the same tool from overlapping (SPEC: 同一
// 時間只允許一個執行個體). It uses an operating-system file lock, so the lock
// disappears with the process even after a crash or power loss and never
// has to be removed by hand.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("another run is in progress")

// Lock is a held lock.
type Lock struct {
	f    *os.File
	path string
}

// Acquire takes the lock file at path, creating it and its directory if
// needed. It does not wait: when another process holds the lock it returns
// an error matching ErrLocked.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		if errors.Is(err, ErrLocked) {
			return nil, fmt.Errorf("%w (lock file %s)", ErrLocked, path)
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &Lock{f: f, path: path}, nil
}

// Release frees the lock. The file itself is left in place; removing it
// could race with another process that has just opened it.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockFile(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }
