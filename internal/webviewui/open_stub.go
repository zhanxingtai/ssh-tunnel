//go:build !windows && !webview

package webviewui

// Available reports whether this build can open a native window.
func Available() bool { return false }

// Open is not supported in this build; the caller falls back to the system
// browser.
func Open(url string) error { return ErrUnsupported }

// OpenWith is not supported in this build.
func OpenWith(url string, opts Options) error { return ErrUnsupported }

// OpenOrShow is not supported in this build.
func OpenOrShow(url string) error { return ErrUnsupported }

// Show does nothing in this build.
func Show() {}

// IsOpen tells whether a native window is open.
func IsOpen() bool { return false }

// Close does nothing in this build.
func Close() {}
