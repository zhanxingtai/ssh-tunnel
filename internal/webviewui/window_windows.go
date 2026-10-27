//go:build windows

package webviewui

import (
	"reflect"
	"sync"
	"unsafe"

	webview "github.com/jchv/go-webview-selector"
	"golang.org/x/sys/windows"

	"ssh-tunnel/internal/console"
)

// The messages and the values used to control the window.
const (
	wmClose = 0x0010
	wmQuit  = 0x0012

	swHide    = 0
	swShow    = 5
	swRestore = 9
)

// The values of the thread-local hook that keeps the webview window hidden
// while it is created.
const (
	whCBT         = 5
	hcbtCreateWnd = 3
	hcbtActivate  = 5
	hcbtSetFocus  = 9

	wmWindowPosChanging = 0x0046
	swpShowWindow       = 0x0040
)

// windowPos is the Win32 WINDOWPOS structure. Only the flags are used.
type windowPos struct {
	hwnd            uintptr
	hwndInsertAfter uintptr
	x               int32
	y               int32
	cx              int32
	cy              int32
	flags           uint32
}

// gwlpWndProc is the index of the window procedure in the window.
var gwlpWndProc = int32(-4)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	pGetWindowLongPtrW      = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW      = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW        = user32.NewProc("CallWindowProcW")
	pShowWindow             = user32.NewProc("ShowWindow")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	pPostThreadMessageW     = user32.NewProc("PostThreadMessageW")
	pSetWindowsHookExW      = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx    = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx         = user32.NewProc("CallNextHookEx")
)

// The window procedure of the webview window is replaced, so that closing
// the window hides it instead of ending the program. The original procedure
// is kept here.
var (
	procMu        sync.Mutex
	originalProc  uintptr
	installedHwnd uintptr
	hideOnClose   bool
)

var closeHandlerCallback = windows.NewCallback(closeHandlerProc)

// installCloseHandler replaces the window procedure of the window.
//
// With hide the window is only hidden when the user closes it; the program
// keeps running and the window can be shown again.
func installCloseHandler(hwnd uintptr, hide bool) {
	if hwnd == 0 {
		return
	}
	procMu.Lock()
	defer procMu.Unlock()
	hideOnClose = hide
	if installedHwnd == hwnd {
		// The window is already handled.
		return
	}
	original, _, _ := pGetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc))
	if original == 0 {
		return
	}
	originalProc = original
	installedHwnd = hwnd
	pSetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc), uintptr(closeHandlerCallback))
}

// closeHandlerProc is the window procedure of the webview window.
func closeHandlerProc(hwnd, message, wParam, lParam uintptr) (result uintptr) {
	// A panic inside a callback would end the program, so it is caught
	// here.
	defer func() {
		if r := recover(); r != nil {
			console.Errorln("error: the window failed:", r)
		}
	}()

	procMu.Lock()
	hide := hideOnClose
	original := originalProc
	procMu.Unlock()

	if message == wmClose && hide {
		// Hide the window instead of destroying it: the program stays in
		// the notification area.
		pShowWindow.Call(hwnd, swHide)
		return 0
	}
	if original == 0 {
		return 0
	}
	out, _, _ := pCallWindowProcW.Call(original, hwnd, message, wParam, lParam)
	return out
}

// showWindow brings the window to the front.
func showWindow(hwnd uintptr) {
	pShowWindow.Call(hwnd, swRestore)
	pShowWindow.Call(hwnd, swShow)
	pSetForegroundWindow.Call(hwnd)
}

// showWebview makes the WebView2 controller visible. When the parent window
// is hidden while the controller is created, the controller stays invisible;
// showing the parent alone does not make it render. The go-webview2 wrapper
// does not expose the controller, so it is looked up in the concrete webview.
func showWebview(w webview.WebView) {
	value := reflect.ValueOf(w)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return
	}
	field := value.Elem().FieldByName("browser")
	if !field.IsValid() {
		return
	}
	browser := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
	if controller, ok := browser.(interface{ Show() error }); ok {
		_ = controller.Show()
	}
}

// hideWindow hides the window without ending its message loop.
func hideWindow(hwnd uintptr) {
	if hwnd != 0 {
		pShowWindow.Call(hwnd, swHide)
	}
}

// webviewWindowClass is the class name that go-webview2 uses for its window.
const webviewWindowClass = "webview"

// isWebviewWindow tells whether the window has the class used by
// go-webview2.
func isWebviewWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var class [64]uint16
	n, _ := windows.GetClassName(windows.HWND(hwnd), &class[0], int32(len(class)))
	return n > 0 && windows.UTF16ToString(class[:int(n)]) == webviewWindowClass
}

var (
	hookMu     sync.Mutex
	hookTarget uintptr

	startupMu       sync.Mutex
	startupHwnd     uintptr
	startupOriginal uintptr
)

var (
	cbtHookCallback     = windows.NewCallback(cbtHookProc)
	startupProcCallback = windows.NewCallback(startupProc)
)

// startupProc keeps the window hidden while go-webview2 creates it.
func startupProc(hwnd, msg, wParam uintptr, lParam unsafe.Pointer) uintptr {
	if msg == wmWindowPosChanging {
		pos := (*windowPos)(lParam)
		pos.flags &^= swpShowWindow
	}
	startupMu.Lock()
	original := startupOriginal
	startupMu.Unlock()
	if original == 0 {
		return 0
	}
	out, _, _ := pCallWindowProcW.Call(original, hwnd, msg, wParam, uintptr(lParam))
	return out
}

// blockStartupShow replaces the window procedure before go-webview2 shows
// the window for the first time.
func blockStartupShow(hwnd uintptr) {
	original, _, _ := pGetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc))
	if original == 0 {
		return
	}
	startupMu.Lock()
	startupHwnd = hwnd
	startupOriginal = original
	startupMu.Unlock()
	pSetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc), startupProcCallback)
}

// restoreStartupProc puts the original window procedure back.
func restoreStartupProc() {
	startupMu.Lock()
	hwnd := startupHwnd
	original := startupOriginal
	startupHwnd = 0
	startupOriginal = 0
	startupMu.Unlock()
	if hwnd != 0 && original != 0 {
		pSetWindowLongPtrW.Call(hwnd, uintptr(gwlpWndProc), original)
	}
}

// cbtHookProc hides the webview window when it is first activated. The hook
// runs on the thread that creates the window, so the window never becomes
// visible.
func cbtHookProc(code int32, wParam, lParam uintptr) uintptr {
	switch code {
	case hcbtCreateWnd:
		if isWebviewWindow(wParam) {
			hookMu.Lock()
			hookTarget = wParam
			hookMu.Unlock()
			blockStartupShow(wParam)
		}
	case hcbtActivate, hcbtSetFocus:
		hookMu.Lock()
		target := hookTarget
		hookMu.Unlock()
		if target != 0 && wParam == target {
			hideWindow(target)
		}
	}
	out, _, _ := pCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return out
}

// hideWhileCreating hides the webview window while go-webview2 creates and
// shows it. The returned function stops hiding it.
func hideWhileCreating() (stop func()) {
	threadID := windows.GetCurrentThreadId()
	hook, _, _ := pSetWindowsHookExW.Call(whCBT, cbtHookCallback, 0, uintptr(threadID))
	if hook == 0 {
		return func() {}
	}
	return func() {
		restoreStartupProc()
		pUnhookWindowsHookEx.Call(hook)
		hookMu.Lock()
		hookTarget = 0
		hookMu.Unlock()
	}
}

// closeWindow ends the message loop of the window, so that the program can
// go on with its exit.
func closeWindow(hwnd uintptr) {
	var processID uint32
	thread, _, _ := pGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&processID)))
	if thread == 0 {
		return
	}
	pPostThreadMessageW.Call(thread, wmQuit, 0, 0)
}
