//go:build windows

package webviewui

import (
	"unsafe"

	"ssh-tunnel/internal/assets"
)

// The values of the Win32 calls used to set the window icon.
const (
	wmSetIcon  = 0x0080
	iconSmall  = 0
	iconBig    = 1
	smCXSMICON = 49
	smCYSMICON = 50
)

var (
	procSendMessageW  = user32.NewProc("SendMessageW")
	procGetSystemSize = user32.NewProc("GetSystemMetrics")
)

// setWindowIcon gives the window the icon of the program.
func setWindowIcon(hwnd unsafe.Pointer) {
	if hwnd == nil {
		return
	}
	big, err := assets.LoadIcon(0, 0)
	if err != nil {
		return
	}
	small, err := assets.LoadIcon(systemSize(smCXSMICON), systemSize(smCYSMICON))
	if err != nil {
		small = big
	}
	handle := uintptr(hwnd)
	procSendMessageW.Call(handle, wmSetIcon, iconBig, uintptr(big))
	procSendMessageW.Call(handle, wmSetIcon, iconSmall, uintptr(small))
}

// systemSize returns a system metric, for example the size of the small
// icon.
func systemSize(index int) uint32 {
	result, _, _ := procGetSystemSize.Call(uintptr(index))
	return uint32(result)
}
