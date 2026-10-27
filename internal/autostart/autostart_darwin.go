//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

import "ssh-tunnel/internal/i18n"

// launchdLabel is the label of the launchd job of this tool.
const launchdLabel = "com.ssh-tunnel"

// supportedKinds lists the mechanisms available on macOS.
func supportedKinds() []Kind {
	return []Kind{KindLaunchd}
}

// detectKind recommends a mechanism for the current environment.
func detectKind() Kind {
	return KindLaunchd
}

func enabledKind(kind Kind) (bool, error) {
	switch kind {
	case KindLaunchd:
		path, err := launchdFile()
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
	default:
		return false, i18n.E("err.unknown_autostart_kind_short", kind)
	}
}

func setKind(kind Kind, enabled bool) error {
	switch kind {
	case KindLaunchd:
		return setLaunchd(enabled)
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
	if kind != KindLaunchd {
		return ""
	}
	path, err := launchdFile()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// The command is the string before the "daemon" argument.
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "<string>daemon</string>") || i == 0 {
			continue
		}
		previous := strings.TrimSpace(lines[i-1])
		if !strings.HasPrefix(previous, "<string>") {
			return ""
		}
		value := strings.TrimSuffix(strings.TrimPrefix(previous, "<string>"), "</string>")
		return value + " daemon"
	}
	return ""
}

// setLaunchd writes the launch agent of this tool and loads it.
//
// The job is a launch agent of the user: it starts when the user logs in.
func setLaunchd(enabled bool) error {
	path, err := launchdFile()
	if err != nil {
		return err
	}
	if !enabled {
		_ = launchctl("bootout", launchdTarget())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return i18n.E("err.remove_launchd_agent", err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return i18n.E("err.locate_executable", err)
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
</dict>
</plist>
`, launchdLabel, exe)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return i18n.E("err.create_autostart_dir", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return i18n.E("err.write_launchd_agent", err)
	}
	// Ask launchd to pick the job up right away. The job is only loaded for
	// the rest of the session; the file makes it start with the next login.
	_ = launchctl("bootstrap", "gui/"+launchdUID(), path)
	return nil
}

// launchdFile returns the path of the launch agent of this tool.
func launchdFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.E("err.locate_home", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

// launchdUID returns the identifier of the user, which names the domain of
// the launch agent.
func launchdUID() string {
	if uid := os.Getuid(); uid >= 0 {
		return strconv.Itoa(uid)
	}
	return ""
}

// launchdTarget returns the name of the job inside the domain of the user.
func launchdTarget() string {
	return "gui/" + launchdUID() + "/" + launchdLabel
}

// launchctl runs a launchctl command.
func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return i18n.E("err.launchctl", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// StartService starts the registered launch agent.
func StartService(kind Kind) error {
	switch kind {
	case KindLaunchd:
		return launchctl("kickstart", "-k", launchdTarget())
	default:
		return i18n.E("err.no_service_manager", kind)
	}
}

// StopService stops the registered launch agent.
func StopService(kind Kind) error {
	switch kind {
	case KindLaunchd:
		return launchctl("kill", "SIGTERM", launchdTarget())
	default:
		return i18n.E("err.no_service_manager", kind)
	}
}

// ServiceStatus reports whether the registered launch agent is running.
func ServiceStatus(kind Kind) (string, error) {
	switch kind {
	case KindLaunchd:
		out, err := exec.Command("launchctl", "list", launchdLabel).CombinedOutput()
		if err != nil {
			// launchctl exits non-zero when the job is not loaded.
			return "stopped", nil
		}
		// The first column holds the process identifier, a dash when the
		// job is not running.
		fields := strings.Fields(string(out))
		if len(fields) > 0 && fields[0] != "-" {
			return "running", nil
		}
		return "stopped", nil
	default:
		return "", i18n.E("err.no_service_manager", kind)
	}
}
