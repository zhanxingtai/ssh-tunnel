//go:build windows

package singleinstance

import (
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

// The values used by the event that carries the request to show the window.
const (
	eventModifyState = 0x0002
	waitInfinite     = 0xFFFFFFFF
	waitObject       = 0x00000000
)

// WatchShow calls show whenever another instance asks the running one to
// show its window. The returned function stops the watching.
func WatchShow(name string, show func()) (func(), error) {
	eventName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	// The event is reset by hand, so that every request is answered.
	handle, err := windows.CreateEvent(nil, 1, 0, eventName)
	if err != nil {
		return nil, err
	}

	stop := make(chan struct{})
	var once sync.Once
	go func() {
		// The waiting blocks a thread, so it stays on the same one.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer windows.CloseHandle(handle)
		for {
			result, err := windows.WaitForSingleObject(handle, waitInfinite)
			if err != nil || result != waitObject {
				return
			}
			windows.ResetEvent(handle)
			select {
			case <-stop:
				return
			default:
			}
			show()
		}
	}()

	return func() {
		once.Do(func() {
			close(stop)
			// Wake the waiting up so that it can end.
			windows.SetEvent(handle)
		})
	}, nil
}

// SignalShow asks the instance that is running to show its window. It
// returns false when there is no such instance.
func SignalShow(name string) bool {
	eventName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	handle, err := windows.OpenEvent(eventModifyState, false, eventName)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	return windows.SetEvent(handle) == nil
}
