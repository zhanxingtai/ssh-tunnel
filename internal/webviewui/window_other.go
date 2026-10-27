//go:build !windows

package webviewui

// installCloseHandler does nothing outside Windows: the other window
// toolkits end the program when the window is closed.
func installCloseHandler(hwnd uintptr, hide bool) {}

// showWindow does nothing outside Windows.
func showWindow(hwnd uintptr) {}

// hideWindow does nothing outside Windows.
func hideWindow(hwnd uintptr) {}

// hideWhileCreating does nothing outside Windows.
func hideWhileCreating() func() { return func() {} }

// closeWindow does nothing outside Windows.
func closeWindow(hwnd uintptr) {}
