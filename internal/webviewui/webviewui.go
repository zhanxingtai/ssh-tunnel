// Package webviewui opens the management page in a native window backed by
// the webview engine of the operating system.
//
// The window is a thin shell: the page itself is served by the local HTTP
// server of the gui package, so the browser and the window show exactly the
// same interface.
package webviewui

import "ssh-tunnel/internal/i18n"

// ErrUnsupported is returned when this build cannot open a native window.
var ErrUnsupported = i18n.E("err.native_window_unavailable")

// Title is the window title.
const Title = "SSH-Tunnel"

// DefaultWidth and DefaultHeight are the initial window size.
const (
	DefaultWidth  = 1080
	DefaultHeight = 720
)

// Options controls the native window.
type Options struct {
	// HideOnClose keeps the window in the background when the user closes
	// it: the program keeps running and the window can be shown again.
	// Only Windows can do this.
	HideOnClose bool
}
