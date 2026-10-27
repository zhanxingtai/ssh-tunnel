//go:build windows

package singleinstance

import (
	"errors"

	"golang.org/x/sys/windows"
)

import "ssh-tunnel/internal/i18n"

// Acquire creates a named mutex. When the mutex already exists another
// instance is running and ErrAlreadyRunning is returned.
func Acquire(name, lockFile string) (*Guard, error) {
	handle, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(name))
	if handle == 0 {
		return nil, i18n.E("err.create_mutex", name, err)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(handle)
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		windows.CloseHandle(handle)
		return nil, i18n.E("err.create_mutex", name, err)
	}
	return &Guard{release: func() { windows.CloseHandle(handle) }}, nil
}
