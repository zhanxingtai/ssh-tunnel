//go:build !windows

package main

// prepareConsole does nothing outside Windows: a program started from a
// terminal already has the handles of that terminal.
func prepareConsole() {}
