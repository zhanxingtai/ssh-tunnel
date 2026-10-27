// Package tray shows a system tray icon with a menu to control the tunnels.
// The tray is implemented on Windows; other platforms get a stub.
package tray

import "ssh-tunnel/internal/i18n"

// ErrUnsupported is returned when the platform has no tray implementation.
var ErrUnsupported = i18n.E("err.tray_unsupported")

// Options configures the tray menu.
type Options struct {
	// OpenGUI opens the graphical configuration interface.
	OpenGUI func()
	// Quit terminates the application.
	Quit func()
}

// Available reports whether the platform has a system tray.
func Available() bool { return trayAvailable }
