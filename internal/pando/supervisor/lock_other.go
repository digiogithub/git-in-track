//go:build !unix

package supervisor

import (
	"errors"
	"io/fs"
	"os"
)

// dirLock on platforms without flock is a create-exclusive file removed on
// release. A crash leaves it behind, which is one reason managed mode is not
// supported on these platforms yet (see the package documentation).
type dirLock struct{ path string }

func acquireLock(path string) (*dirLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, ErrLocked
		}
		return nil, err
	}
	_ = f.Close()
	return &dirLock{path: path}, nil
}

func (l *dirLock) release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
	l.path = ""
}
