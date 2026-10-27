//go:build windows || webview

package webviewui

import (
	"runtime"
	"sync"
	"time"

	webview "github.com/jchv/go-webview-selector"
)

import (
	"ssh-tunnel/internal/console"
	"ssh-tunnel/internal/i18n"
)

// The state of the window that is open. The window procedure of the native
// window is a callback, so the handle is kept here.
var (
	stateMu    sync.Mutex
	windowHwnd uintptr

	// openMu makes sure that there is only one window.
	openMu sync.Mutex
)

// Available reports whether this build can open a native window.
func Available() bool { return true }

// Open shows the management page in a native window and blocks until the
// window is closed.
func Open(url string) error {
	return OpenWith(url, Options{})
}

// OpenWith shows the management page in a native window and blocks until the
// window is closed.
//
// With HideOnClose the window stays in the background when the user closes
// it: the program keeps running and the window can be shown again.
func OpenWith(url string, opts Options) error {
	openMu.Lock()
	defer openMu.Unlock()
	return openWindow(url, opts)
}

// OpenOrShow brings the window to the front when it is open and starts it
// in the background otherwise. It does not block.
func OpenOrShow(url string) error {
	stateMu.Lock()
	hwnd := windowHwnd
	stateMu.Unlock()
	if hwnd != 0 {
		showWindow(hwnd)
		return nil
	}

	go func() {
		openMu.Lock()
		defer openMu.Unlock()
		// Another window may have been opened in the meantime.
		stateMu.Lock()
		hwnd := windowHwnd
		stateMu.Unlock()
		if hwnd != 0 {
			showWindow(hwnd)
			return
		}
		if err := openWindow(url, Options{HideOnClose: true}); err != nil {
			console.Errorln(i18n.T("msg.error_line", err))
		}
	}()
	return nil
}

// Show brings the window to the front. It does nothing when no window is
// open.
func Show() {
	stateMu.Lock()
	hwnd := windowHwnd
	stateMu.Unlock()
	if hwnd != 0 {
		showWindow(hwnd)
	}
}

// IsOpen tells whether a native window is open.
func IsOpen() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return windowHwnd != 0
}

// Close ends the native window; the program exits when the window was the
// only thing it was doing.
func Close() {
	stateMu.Lock()
	hwnd := windowHwnd
	stateMu.Unlock()
	if hwnd != 0 {
		closeWindow(hwnd)
	}
}

// openWindow creates the window and runs its message loop until the window
// ends.
func openWindow(url string, opts Options) (err error) {
	// The window and the message loop that serves it have to stay on the
	// same thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if r := recover(); r != nil {
			err = i18n.E("err.webview_failed", r)
		}
	}()

	// The webview shows its window before the page is loaded, which looks
	// like a small white box for a moment. Keep the window hidden while the
	// webview is created and show it once the page has loaded.
	stopHiding := hideWhileCreating()
	w := func() webview.WebView {
		defer stopHiding()
		return webview.New(false)
	}()
	if w == nil {
		return i18n.E("err.webview_create")
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	showWebview(w)
	hideWindow(hwnd)

	w.SetTitle(Title)
	w.SetSize(DefaultWidth, DefaultHeight, webview.HintNone)
	setWindowIcon(w.Window())

	installCloseHandler(hwnd, opts.HideOnClose)

	stateMu.Lock()
	windowHwnd = hwnd
	stateMu.Unlock()
	defer func() {
		stateMu.Lock()
		windowHwnd = 0
		stateMu.Unlock()
	}()

	revealWindow := func() {
		w.Dispatch(func() {
			showWebview(w)
			showWindow(hwnd)
		})
	}

	reveal := make(chan struct{}, 1)
	stopReveal := make(chan struct{})
	defer close(stopReveal)
	if err := w.Bind("__sshTunnelReady", func() {
		select {
		case reveal <- struct{}{}:
		default:
		}
	}); err != nil {
		// Without the binding the page cannot report that it is ready.
		revealWindow()
	} else {
		w.Init(`window.addEventListener("load", function () {
			if (window.__sshTunnelReady) {
				window.__sshTunnelReady();
			}
		});`)
		go func() {
			timer := time.NewTimer(10 * time.Second)
			defer timer.Stop()
			select {
			case <-reveal:
			case <-timer.C:
			case <-stopReveal:
				return
			}
			revealWindow()
		}()
	}

	w.Navigate(url)
	w.Run()
	return nil
}
