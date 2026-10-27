//go:build !windows

// Package console writes the output of the command line to the terminal.
package console

import (
	"fmt"
	"os"
)

// Println writes a line to the standard output.
func Println(args ...any) {
	fmt.Println(args...)
}

// Printf writes a formatted message to the standard output.
func Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}

// Errorln writes a line to the standard error.
func Errorln(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
}

// Errorf writes a formatted message to the standard error.
func Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
