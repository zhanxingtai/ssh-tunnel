//go:build windows

package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

import "ssh-tunnel/internal/i18n"

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// supportedKinds lists the mechanisms available on Windows.
func supportedKinds() []Kind {
	return []Kind{KindRegistry}
}

// detectKind returns the only mechanism available on Windows.
func detectKind() Kind { return KindRegistry }

func enabledKind(kind Kind) (bool, error) {
	if kind != KindRegistry {
		return false, i18n.E("err.unknown_autostart_kind_short", kind)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, i18n.E("err.open_run_key", err)
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(Name); err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, i18n.E("err.read_autostart_value", err)
	}
	return true, nil
}

func setKind(kind Kind, enabled bool) error {
	if kind != KindRegistry {
		return i18n.E("err.unknown_autostart_kind_short", kind)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return i18n.E("err.open_run_key", err)
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(Name); err != nil && err != registry.ErrNotExist {
			return i18n.E("err.delete_autostart_value", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return i18n.E("err.locate_executable", err)
	}
	command := fmt.Sprintf("\"%s\" tray", exe)
	if err := k.SetStringValue(Name, command); err != nil {
		return i18n.E("err.write_autostart_value", err)
	}
	return nil
}

// commandKind returns the command of a mechanism that is not registered yet.
func commandKind(kind Kind) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\"%s\" tray", exe)
}

func describeKind(kind Kind) string {
	if kind != KindRegistry {
		return ""
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	cmd, _, err := k.GetStringValue(Name)
	if err != nil {
		return ""
	}
	return cmd
}

// StartService is not available on Windows: the registry entry starts the
// application itself, which the user launches directly.
func StartService(kind Kind) error {
	return i18n.E("err.windows_no_service_manager", Name)
}

// StopService is not available on Windows.
func StopService(kind Kind) error {
	return i18n.E("err.windows_no_service_manager_stop", Name)
}

// ServiceStatus is not available on Windows.
func ServiceStatus(kind Kind) (string, error) {
	return "", i18n.E("err.no_service_manager_short")
}
