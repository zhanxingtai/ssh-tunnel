//go:build windows

package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"ssh-tunnel/internal/assets"
	"ssh-tunnel/internal/console"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/manager"
)

// The messages of the tray window.
const (
	wmDestroy     = 0x0002
	wmNull        = 0x0000
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000
)

// wmCallback is the message the system sends when the mouse is used on the
// tray icon.
const wmCallback = wmApp + 1

// The commands of the tray menu.
const (
	menuOpen = iota + 1
	menuQuit
)

// The flags of the menu items.
const (
	mfString    = 0x0000
	mfChecked   = 0x0008
	mfSeparator = 0x0800
)

// The flags of the popup menu.
const (
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
)

// The calls of the notification area.
const (
	nimAdd        = 0x00000000
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000006

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	// notifVersion tells the system to send the double click as well.
	notifVersion = 4
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
)

// wndClassEx is the WNDCLASSEXW structure.
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

// windowMessage is the MSG structure. The empty fields are the padding
// that Win32 expects.
type windowMessage struct {
	hwnd    windows.Handle
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	_       uint32
	point   struct{ x, y int32 }
}

// notifyIconData is the NOTIFYICONDATAW structure.
type notifyIconData struct {
	size            uint32
	_               uint32
	wnd             windows.Handle
	id              uint32
	flags           uint32
	callbackMessage uint32
	_               uint32
	icon            windows.Handle
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	timeout         uint32
	version         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        windows.GUID
	_               uint32
	balloonIcon     windows.Handle
}

// The window procedure is a callback, so it cannot take arguments: the
// state of the tray lives in package variables.
var (
	stateMu     sync.Mutex
	currentMgr  *manager.Manager
	currentOpts Options
	trayIcon    windows.Handle
	taskbarMsg  uint32
)

var wndProcCallback = windows.NewCallback(trayWndProc)

// trayAvailable tells that Windows has a system tray.
const trayAvailable = true

// Run shows the tray icon and blocks until the user quits the application.
//
// The icon reacts to the mouse in the usual way: a double click opens the
// interface, the right button shows the menu.
func Run(mgr *manager.Manager, opts Options) error {
	// The hidden window and its message loop must stay on one thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	stateMu.Lock()
	currentMgr = mgr
	currentOpts = opts
	stateMu.Unlock()

	icon, err := assets.LoadIcon(0, 0)
	if err != nil {
		return fmt.Errorf("cannot load the tray icon: %w", err)
	}
	trayIcon = icon

	className, err := windows.UTF16PtrFromString("ssh-tunnel-tray")
	if err != nil {
		return err
	}
	instance, _, _ := pGetModuleHandleW.Call(0)
	class := wndClassEx{
		size:      uint32(unsafe.Sizeof(wndClassEx{})),
		style:     0x0008, // CS_DBLCLKS, so that the double click arrives
		wndProc:   wndProcCallback,
		instance:  windows.Handle(instance),
		icon:      icon,
		iconSm:    icon,
		className: className,
	}
	if atom, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		// The class can already exist when the tray is started twice.
		if !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
			return fmt.Errorf("cannot register the tray window class: %w", err)
		}
	}

	hwnd, _, _ := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0, // no window text
		0, // no style: the window stays hidden
		0, 0, 0, 0,
		0, // no parent
		0, // no menu
		uintptr(instance),
		0,
	)
	if hwnd == 0 {
		return i18n.E("err.tray_window_create")
	}

	// The system sends a message when it restarts; the icon has to be
	// added again then.
	taskbarName := mustUTF16("TaskbarCreated")
	name, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(&taskbarName[0])))
	taskbarMsg = uint32(name)

	addIcon(windows.Handle(hwnd))
	defer removeIcon(windows.Handle(hwnd))

	var msg windowMessage
	for {
		result, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return i18n.E("err.tray_message_loop")
		}
		if int32(result) == 0 {
			// WM_QUIT
			return nil
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// trayWndProc handles the messages of the hidden tray window.
func trayWndProc(hwnd, message, wParam, lParam uintptr) (result uintptr) {
	// A panic inside a callback would end the program, so it is caught
	// here.
	defer func() {
		if r := recover(); r != nil {
			console.Errorln("error: the tray failed:", r)
		}
	}()

	switch message {
	case wmCallback:
		switch lParam {
		case wmLButtonDbl:
			openInterface()
		case wmRButtonUp, wmContextMenu:
			showMenu(hwnd)
		default:
			// A single click does nothing: the interface opens on a
			// double click.
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	if taskbarMsg != 0 && message == uintptr(taskbarMsg) {
		// The system restarted; the icon has to be added again.
		addIcon(windows.Handle(hwnd))
		return 0
	}
	out, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return out
}

// addIcon puts the icon into the notification area.
func addIcon(hwnd windows.Handle) {
	data := notifyIconData{
		size:            uint32(unsafe.Sizeof(notifyIconData{})),
		wnd:             hwnd,
		id:              1,
		flags:           nifMessage | nifIcon | nifTip,
		callbackMessage: wmCallback,
		icon:            trayIcon,
	}
	setText(&data.tip, i18n.T("tray.tip"))
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data)))

	// Without this the system does not send the double click.
	data.version = notifVersion
	pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&data)))
}

// removeIcon takes the icon out of the notification area.
func removeIcon(hwnd windows.Handle) {
	data := notifyIconData{
		size: uint32(unsafe.Sizeof(notifyIconData{})),
		wnd:  hwnd,
		id:   1,
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
}

// showMenu shows the menu of the tray icon and runs the chosen command.
func showMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)

	appendItem(menu, menuOpen, i18n.T("tray.open"), mfString)
	appendItem(menu, 0, "", mfSeparator)
	appendItem(menu, menuQuit, i18n.T("tray.quit"), mfString)

	var point struct{ x, y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	// The menu has to disappear when it is clicked away, so the window
	// comes to the front first.
	pSetForegroundWindow.Call(hwnd)
	command, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd,
		uintptr(point.x), uintptr(point.y), 0, hwnd, 0)
	// Let the window know that the menu is gone, otherwise it stays
	// active and the next click is swallowed.
	pPostMessageW.Call(hwnd, wmNull, 0, 0)

	switch command {
	case menuOpen:
		openInterface()
	case menuQuit:
		quit()
	}
}

// appendItem adds one item to the menu.
func appendItem(menu uintptr, id uintptr, text string, flags uintptr) {
	if flags == mfSeparator {
		pAppendMenuW.Call(menu, mfSeparator, 0, 0)
		return
	}
	encoded := mustUTF16(text)
	pAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(&encoded[0])))
}

// openInterface asks for the interface to be shown.
func openInterface() {
	stateMu.Lock()
	opts := currentOpts
	stateMu.Unlock()
	if opts.OpenGUI != nil {
		opts.OpenGUI()
	}
}

// quit ends the program.
func quit() {
	stateMu.Lock()
	opts := currentOpts
	stateMu.Unlock()
	withManager(func(m *manager.Manager) { m.StopAll() })
	if opts.Quit != nil {
		opts.Quit()
	}
	pPostQuitMessage.Call(0)
}

// withManager runs the action with the manager of the tray.
func withManager(action func(*manager.Manager)) {
	stateMu.Lock()
	mgr := currentMgr
	stateMu.Unlock()
	if mgr != nil {
		action(mgr)
	}
}

// setText copies a text into a UTF-16 buffer.
func setText(buffer *[128]uint16, text string) {
	encoded := mustUTF16(text)
	copy(buffer[:], encoded)
}

// mustUTF16 encodes a text; a text that cannot be encoded is empty.
func mustUTF16(text string) []uint16 {
	encoded, err := windows.UTF16FromString(text)
	if err != nil {
		return []uint16{0}
	}
	return encoded
}
