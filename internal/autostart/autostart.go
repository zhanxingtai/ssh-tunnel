// Package autostart registers the tool so that it starts with the operating
// system.
package autostart

import (
	"sort"
)

import "ssh-tunnel/internal/i18n"

// ErrUnsupported is returned on platforms without autostart support.
var ErrUnsupported = i18n.E("err.autostart_unsupported")

// Name is the registry key / unit name / desktop file entry name.
const Name = "ssh-tunnel"

// Kind is a way of registering the tool for autostart.
type Kind string

// The supported autostart mechanisms.
const (
	// KindDesktop is the XDG autostart entry; it needs a desktop session.
	KindDesktop Kind = "desktop"
	// KindSystemdUser is a systemd user service; it needs a user manager
	// and, for a start before any login, lingering enabled.
	KindSystemdUser Kind = "systemd-user"
	// KindSystemdSystem is a systemd system service; installing it needs
	// administrator rights.
	KindSystemdSystem Kind = "systemd-system"
	// KindCron is a crontab @reboot entry.
	KindCron Kind = "cron"
	// KindRegistry is the Windows Run key.
	KindRegistry Kind = "registry"
)

// DefaultKind returns the mechanism used when the caller does not pick one.
func DefaultKind() Kind {
	return detectKind()
}

// Kinds returns the mechanisms supported on this platform, sorted.
func Kinds() []Kind {
	kinds := supportedKinds()
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// ParseKind validates a mechanism name.
func ParseKind(name string) (Kind, error) {
	for _, k := range supportedKinds() {
		if string(k) == name {
			return k, nil
		}
	}
	names := make([]string, 0, len(supportedKinds()))
	for _, k := range supportedKinds() {
		names = append(names, string(k))
	}
	return "", i18n.E("err.unknown_autostart_kind", name, names)
}

// Enabled reports whether the autostart entry of the given kind exists.
func Enabled(kind Kind) (bool, error) {
	return enabledKind(kind)
}

// Set registers or removes the autostart entry of the given kind.
func Set(kind Kind, enabled bool) error {
	return setKind(kind, enabled)
}

// Describe returns the command used to start the tool automatically.
func Describe(kind Kind) string {
	return describeKind(kind)
}

// Command returns the command used to start the tool with the given
// mechanism. It works even when the entry does not exist yet.
func Command(kind Kind) string {
	if c := describeKind(kind); c != "" {
		return c
	}
	return commandKind(kind)
}
