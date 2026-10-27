//go:build !windows

package tray

import "ssh-tunnel/internal/manager"

// Run reports that the tray is not available on this platform.
func Run(mgr *manager.Manager, opts Options) error {
	return ErrUnsupported
}

// trayAvailable tells that the other platforms have no system tray.
const trayAvailable = false
