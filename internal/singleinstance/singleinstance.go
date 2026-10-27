// Package singleinstance makes sure only one background instance of the tool
// runs at a time. On Windows a named mutex is used, on unix-like systems a
// locked file.
package singleinstance

import "ssh-tunnel/internal/i18n"

// ErrAlreadyRunning is returned when another instance already holds the lock.
var ErrAlreadyRunning = i18n.E("err.instance_already_running")

// Guard represents the ownership of the single instance lock.
type Guard struct {
	release func()
}

// Release gives the lock back. It is safe to call more than once.
func (g *Guard) Release() {
	if g == nil || g.release == nil {
		return
	}
	g.release()
}
