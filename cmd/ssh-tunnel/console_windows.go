//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess asks AttachConsole to use the console of the parent
// process. It is the value (DWORD)-1.
const attachParentProcess = ^uint32(0)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// prepareConsole gives the program the console of the terminal it was started
// from. The release build uses the Windows GUI subsystem, so it does not get a
// console on its own; without this the output of the command line would be
// lost. When there is no parent console the program keeps its own handles,
// which may be pipes or nothing at all.
func prepareConsole() {
	outOK := hasStdHandle(windows.STD_OUTPUT_HANDLE)
	errOK := hasStdHandle(windows.STD_ERROR_HANDLE)
	inOK := hasStdHandle(windows.STD_INPUT_HANDLE)
	if outOK && errOK {
		return
	}
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if !outOK {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = f
		}
	}
	if !errOK {
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = f
		}
	}
	if !inOK {
		if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
			os.Stdin = f
		}
	}
}

func hasStdHandle(id uint32) bool {
	h, err := windows.GetStdHandle(id)
	return err == nil && h != 0 && h != windows.InvalidHandle
}
