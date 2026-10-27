//go:build windows

// Package console writes the output of the command line to the terminal.
//
// Go writes UTF-8 bytes, but a Windows console interprets them with its active
// code page. A Chinese Windows installation uses code page 936, so the text
// would be garbled. When the output is a console, the messages are therefore
// written with WriteConsoleW, which takes Unicode directly and ignores the
// code page. Pipes and files still receive UTF-8.
package console

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// maxConsoleChars is the largest buffer WriteConsoleW accepts in one call.
const maxConsoleChars = 32767

var outputMu sync.Mutex

// Println writes a line to the standard output.
func Println(args ...any) {
	write(os.Stdout, fmt.Sprintln(args...))
}

// Printf writes a formatted message to the standard output.
func Printf(format string, args ...any) {
	write(os.Stdout, fmt.Sprintf(format, args...))
}

// Errorln writes a line to the standard error.
func Errorln(args ...any) {
	write(os.Stderr, fmt.Sprintln(args...))
}

// Errorf writes a formatted message to the standard error.
func Errorf(format string, args ...any) {
	write(os.Stderr, fmt.Sprintf(format, args...))
}

// write sends a message to a stream. A console receives UTF-16 through
// WriteConsoleW; every other stream receives UTF-8.
func write(file *os.File, message string) {
	outputMu.Lock()
	defer outputMu.Unlock()

	handle := windows.Handle(file.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) == nil {
		if err := writeConsole(handle, message); err == nil {
			return
		}
	}
	_, _ = io.WriteString(file, message)
}

// writeConsole writes a message to a console as UTF-16.
func writeConsole(handle windows.Handle, message string) error {
	chars := utf16.Encode([]rune(message))
	for len(chars) > 0 {
		n := len(chars)
		if n > maxConsoleChars {
			n = maxConsoleChars
			// Never split a surrogate pair between two calls.
			if n > 0 && chars[n-1] >= 0xd800 && chars[n-1] <= 0xdbff {
				n--
			}
		}
		var written uint32
		if err := windows.WriteConsole(handle, &chars[0], uint32(n), &written, nil); err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		chars = chars[written:]
	}
	return nil
}
