//go:build !windows

package webviewui

import "unsafe"

// setWindowIcon does nothing outside Windows: the other platforms take the
// icon of the desktop file or of the window manager.
func setWindowIcon(hwnd unsafe.Pointer) {}
