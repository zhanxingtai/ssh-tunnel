//go:build !windows

package singleinstance

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

import "ssh-tunnel/internal/i18n"

// Acquire locks an exclusive lock on lockFile. When the file is already
// locked by another process ErrAlreadyRunning is returned.
func Acquire(name, lockFile string) (*Guard, error) {
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o700); err != nil {
		return nil, i18n.E("err.create_lock_dir", err)
	}
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, i18n.E("err.open_lock_file", lockFile, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if err == unix.EWOULDBLOCK {
			return nil, ErrAlreadyRunning
		}
		return nil, i18n.E("err.lock_file", lockFile, err)
	}
	return &Guard{release: func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}}, nil
}
