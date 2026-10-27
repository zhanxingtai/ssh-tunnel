//go:build !windows

package singleinstance

// WatchShow does nothing outside Windows: there the running instance shows
// its page in the system browser.
func WatchShow(name string, show func()) (func(), error) {
	return func() {}, nil
}

// SignalShow always fails outside Windows.
func SignalShow(name string) bool { return false }
