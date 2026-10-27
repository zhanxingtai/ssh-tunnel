//go:build windows

package assets

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The values of the Win32 call that loads the icon.
const (
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procLoadImageW = user32.NewProc("LoadImageW")
)

// LoadIcon returns the icon of the program as a Windows icon handle.
//
// Win32 can only load an icon from a file, so the embedded icon is written
// to a temporary file first. The file is removed right away: the icon is
// already in memory at that point. The handle stays valid until the process
// ends.
//
// A size of zero asks for the default size of the icon.
func LoadIcon(width, height uint32) (windows.Handle, error) {
	file, err := os.CreateTemp("", "ssh-tunnel-*.ico")
	if err != nil {
		return 0, err
	}
	path := file.Name()
	_, writeErr := file.Write(iconICO)
	file.Close()
	if writeErr != nil {
		os.Remove(path)
		return 0, writeErr
	}
	defer os.Remove(path)

	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	flags := uintptr(lrLoadFromFile)
	if width == 0 && height == 0 {
		flags |= lrDefaultSize
	}
	handle, _, _ := procLoadImageW.Call(
		0, // the icon comes from a file, not from a module
		uintptr(unsafe.Pointer(name)),
		imageIcon,
		uintptr(width),
		uintptr(height),
		flags,
	)
	if handle == 0 {
		return 0, errors.New("cannot load the icon")
	}
	return windows.Handle(handle), nil
}
