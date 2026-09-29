//go:build unix

package supervisor

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// dirLock is a non-blocking flock on supervisor.lock. The kernel releases it if
// the process dies, so a crashed gintrack never leaves a stale lock.
type dirLock struct{ f *os.File }

func acquireLock(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &dirLock{f: f}, nil
}

func (l *dirLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
