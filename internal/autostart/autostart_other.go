//go:build !windows && !linux

package autostart

import "fmt"

// supportedKinds lists the mechanisms available on this platform.
func supportedKinds() []Kind { return nil }

// detectKind has no recommendation on this platform.
func detectKind() Kind { return "" }

func enabledKind(kind Kind) (bool, error) {
	return false, fmt.Errorf("%w: %s", ErrUnsupported, kind)
}

func setKind(kind Kind, enabled bool) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, kind)
}

func describeKind(kind Kind) string { return "" }

// commandKind has no command on this platform.
func commandKind(kind Kind) string { return "" }

// StartService is not supported on this platform.
func StartService(kind Kind) error { return fmt.Errorf("%w: service", ErrUnsupported) }

// StopService is not supported on this platform.
func StopService(kind Kind) error { return fmt.Errorf("%w: service", ErrUnsupported) }

// ServiceStatus is not supported on this platform.
func ServiceStatus(kind Kind) (string, error) { return "", fmt.Errorf("%w: service", ErrUnsupported) }
