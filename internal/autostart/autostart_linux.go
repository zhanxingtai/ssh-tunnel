//go:build linux

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

import "ssh-tunnel/internal/i18n"

// cronMarker identifies the crontab line owned by this tool.
const cronMarker = "# ssh-tunnel autostart"

// supportedKinds lists the mechanisms available on Linux.
func supportedKinds() []Kind {
	return []Kind{KindCron, KindDesktop, KindSystemdSystem, KindSystemdUser}
}

// detectKind recommends a mechanism for the current environment.
func detectKind() Kind {
	if os.Getenv("XDG_CURRENT_DESKTOP") != "" || os.Getenv("DISPLAY") != "" {
		return KindDesktop
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if err := exec.Command("systemctl", "--user", "is-system-running").Run(); err == nil {
			return KindSystemdUser
		}
	}
	if _, err := exec.LookPath("crontab"); err == nil {
		return KindCron
	}
	return KindSystemdSystem
}

func enabledKind(kind Kind) (bool, error) {
	switch kind {
	case KindDesktop:
		path, err := desktopFile()
		if err != nil {
			return false, err
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	case KindSystemdUser:
		return systemdEnabled(userUnitDir(), "default.target")
	case KindSystemdSystem:
		return systemdEnabled(systemUnitDir(), "multi-user.target")
	case KindCron:
		return cronInstalled()
	default:
		return false, i18n.E("err.unknown_autostart_kind_short", kind)
	}
}

func setKind(kind Kind, enabled bool) error {
	switch kind {
	case KindDesktop:
		return setDesktop(enabled)
	case KindSystemdUser:
		return setSystemdUser(enabled)
	case KindSystemdSystem:
		return setSystemdSystem(enabled)
	case KindCron:
		return setCron(enabled)
	default:
		return i18n.E("err.unknown_autostart_kind_short", kind)
	}
}

// commandKind returns the command of a mechanism that is not registered yet.
func commandKind(kind Kind) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe + " daemon"
}

func describeKind(kind Kind) string {
	switch kind {
	case KindDesktop:
		path, err := desktopFile()
		if err != nil {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Exec=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
			}
		}
		return ""
	case KindSystemdUser, KindSystemdSystem:
		dir := userUnitDir()
		if kind == KindSystemdSystem {
			dir = systemUnitDir()
		}
		path, err := unitFile(dir)
		if err != nil {
			return ""
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ExecStart=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "ExecStart="))
			}
		}
		return ""
	case KindCron:
		entries, err := cronEntries()
		if err != nil {
			return ""
		}
		for _, e := range entries {
			if strings.Contains(e, cronMarker) {
				return strings.TrimSpace(strings.Replace(e, cronMarker, "", 1))
			}
		}
		return ""
	default:
		return ""
	}
}

// setDesktop writes the XDG autostart entry. It only takes effect inside a
// desktop session.
func setDesktop(enabled bool) error {
	path, err := desktopFile()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return i18n.E("err.remove_desktop_entry", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return i18n.E("err.locate_executable", err)
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ssh-tunnel
Comment=SSH port forwarding and SOCKS5 proxy manager
Exec=%s daemon
Terminal=false
X-GNOME-Autostart-enabled=true
`, exe)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return i18n.E("err.create_autostart_dir", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return i18n.E("err.write_desktop_entry", err)
	}
	return nil
}

// setSystemdUser installs the user service and enables it.
//
// The "wants" symlink is created directly instead of calling
// "systemctl --user enable", so the service is enabled even when no user
// manager is running (headless machines, containers, ssh sessions).
func setSystemdUser(enabled bool) error {
	dir := userUnitDir()
	path, err := unitFile(dir)
	if err != nil {
		return err
	}
	if !enabled {
		_ = exec.Command("systemctl", "--user", "disable", "--now", Name+".service").Run()
		removeUnit(dir, "default.target", path)
		return nil
	}
	if err := writeUnit(path, true); err != nil {
		return err
	}
	if err := linkWants(dir, "default.target", path); err != nil {
		return err
	}
	// Ask a running manager to pick the unit up right away.
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

// setSystemdSystem installs the system service. It needs administrator
// rights because the unit lives in /etc/systemd/system.
func setSystemdSystem(enabled bool) error {
	dir := systemUnitDir()
	path, err := unitFile(dir)
	if err != nil {
		return err
	}
	if !enabled {
		_ = exec.Command("systemctl", "disable", "--now", Name+".service").Run()
		removeUnit(dir, "multi-user.target", path)
		return nil
	}
	if err := writeUnit(path, false); err != nil {
		return err
	}
	if err := linkWants(dir, "multi-user.target", path); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// systemdEnabled reports whether the unit is linked into the target.
func systemdEnabled(dir, target string) (bool, error) {
	link := filepath.Join(dir, target+".wants", Name+".service")
	return fileExists(link)
}

// linkWants links the unit into the target, which is what enabling a unit
// means for systemd.
func linkWants(dir, target, unitPath string) error {
	wants := filepath.Join(dir, target+".wants")
	if err := os.MkdirAll(wants, 0o755); err != nil {
		return i18n.E("err.create_wants_dir", err)
	}
	link := filepath.Join(wants, Name+".service")
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return i18n.E("err.remove_old_link", err)
	}
	if err := os.Symlink(filepath.Join("..", Name+".service"), link); err != nil {
		return i18n.E("err.link_unit", err)
	}
	return nil
}

// removeUnit deletes the unit file and its link.
func removeUnit(dir, target, path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return
	}
	os.Remove(filepath.Join(dir, target+".wants", Name+".service"))
}

// writeUnit writes a systemd unit file starting the tunnels.
func writeUnit(path string, userUnit bool) error {
	exe, err := os.Executable()
	if err != nil {
		return i18n.E("err.locate_executable", err)
	}
	content := fmt.Sprintf(`[Unit]
Description=ssh-tunnel SSH port forwarding and SOCKS5 proxy manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=5
`, exe)
	if !userUnit {
		name := currentUser()
		if name != "" {
			content += fmt.Sprintf("User=%s\n", name)
		}
	}
	content += `
[Install]
WantedBy=default.target
`
	if !userUnit {
		content = strings.Replace(content, "WantedBy=default.target", "WantedBy=multi-user.target", 1)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return i18n.E("err.create_unit_dir", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return i18n.E("err.write_unit_file", err)
	}
	return nil
}

// setCron installs an @reboot entry in the user crontab.
func setCron(enabled bool) error {
	entries, err := cronEntries()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, e := range entries {
		if !strings.Contains(e, cronMarker) {
			kept = append(kept, e)
		}
	}
	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return i18n.E("err.locate_executable", err)
		}
		kept = append(kept, fmt.Sprintf("@reboot %s daemon %s", exe, cronMarker))
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(strings.Join(kept, "\n") + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return i18n.E("err.write_crontab", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cronEntries returns the current user crontab lines.
func cronEntries() ([]string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		// No crontab yet is not an error.
		return nil, nil
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	return kept, nil
}

// cronInstalled reports whether the @reboot entry is present.
func cronInstalled() (bool, error) {
	entries, err := cronEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if strings.Contains(e, cronMarker) {
			return true, nil
		}
	}
	return false, nil
}

// desktopFile returns the XDG autostart entry path.
func desktopFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", Name+".desktop"), nil
}

// unitFile returns the unit file path inside dir.
func unitFile(dir string) (string, error) {
	if dir == "" {
		return "", i18n.E("err.no_unit_dir")
	}
	return filepath.Join(dir, Name+".service"), nil
}

// userUnitDir returns the systemd user unit directory.
func userUnitDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "systemd", "user")
}

// systemUnitDir returns the systemd system unit directory.
func systemUnitDir() string { return "/etc/systemd/system" }

// currentUser returns the name of the user running the process.
func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

// fileExists reports whether path exists.
func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// StartService starts the registered systemd service.
func StartService(kind Kind) error {
	switch kind {
	case KindSystemdUser:
		return systemctl("--user", "start", Name+".service")
	case KindSystemdSystem:
		return systemctl("start", Name+".service")
	default:
		return i18n.E("err.no_service_manager", kind)
	}
}

// StopService stops the registered systemd service.
func StopService(kind Kind) error {
	switch kind {
	case KindSystemdUser:
		return systemctl("--user", "stop", Name+".service")
	case KindSystemdSystem:
		return systemctl("stop", Name+".service")
	default:
		return i18n.E("err.no_service_manager", kind)
	}
}

// ServiceStatus reports whether the registered systemd service is running.
func ServiceStatus(kind Kind) (string, error) {
	switch kind {
	case KindSystemdUser:
		return systemctlStatus("--user")
	case KindSystemdSystem:
		return systemctlStatus()
	default:
		return "", i18n.E("err.no_service_manager", kind)
	}
}

// systemctl runs a systemctl command with the given arguments.
func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return i18n.E("err.systemctl", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemctlStatus returns the state of the system service.
func systemctlStatus(user ...string) (string, error) {
	args := []string{"is-active", Name + ".service"}
	if len(user) > 0 {
		args = append([]string{user[0]}, args...)
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	state := strings.TrimSpace(string(out))
	if err != nil {
		// systemctl exits non-zero for a stopped unit but still prints the
		// state; anything else is a real failure.
		switch state {
		case "active", "inactive", "failed", "activating", "deactivating", "unknown", "reloading":
			return state, nil
		}
		return "", i18n.E("err.systemctl", strings.Join(args, " "), err, state)
	}
	return state, nil
}
