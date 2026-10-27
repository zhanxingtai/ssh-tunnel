package main

import (
	"flag"
	"os"

	"ssh-tunnel/internal/autostart"
	"ssh-tunnel/internal/console"
	"ssh-tunnel/internal/daemon"
	"ssh-tunnel/internal/i18n"
)

// runService handles the "service" subcommand: starting, stopping and
// inspecting the registered background instance.
func runService(args []string) error {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	configFile := fs.String("config", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()

	action := ""
	kind := autostart.DefaultKind()
	if len(rest) > 0 {
		action = rest[0]
	}
	switch action {
	case "start", "stop", "restart", "status":
	case "":
		return i18n.E("err.service_requires_action")
	default:
		return i18n.E("err.unknown_service_action", action)
	}
	if len(rest) > 1 {
		k, err := autostart.ParseKind(rest[1])
		if err != nil {
			return err
		}
		kind = k
	}

	// 后台实例要用同一个配置文件。
	childArgs := []string{"daemon"}
	if *configFile != "" {
		childArgs = append(childArgs, "--config", *configFile)
	}

	switch action {
	case "start":
		return serviceStart(kind, childArgs)
	case "stop":
		return serviceStop(kind)
	case "restart":
		if err := serviceStop(kind); err != nil {
			return err
		}
		return serviceStart(kind, childArgs)
	case "status":
		return serviceStatus(kind)
	default:
		return i18n.E("err.unknown_service_action", action)
	}
}

// serviceStart starts the registered instance.
//
// systemd units are started by systemd itself; the other mechanisms have no
// service manager, so the background process is started directly.
func serviceStart(kind autostart.Kind, childArgs []string) error {
	switch kind {
	case autostart.KindSystemdUser, autostart.KindSystemdSystem:
		if err := autostart.StartService(kind); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.service_started", kind))
		return nil
	default:
		exe, err := os.Executable()
		if err != nil {
			return i18n.E("err.locate_executable", err)
		}
		// The registry entry starts the tray, the other mechanisms start
		// the headless daemon.
		command := childArgs
		if kind == autostart.KindRegistry {
			command = []string{"tray"}
		}
		if err := daemon.Start(exe, command...); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.background_started", kind))
		return nil
	}
}

// serviceStop stops the registered instance.
func serviceStop(kind autostart.Kind) error {
	switch kind {
	case autostart.KindSystemdUser, autostart.KindSystemdSystem:
		if err := autostart.StopService(kind); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.service_stopped", kind))
		return nil
	default:
		if err := daemon.Stop(); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.background_stopped", kind))
		return nil
	}
}

// serviceStatus reports whether the instance is running.
func serviceStatus(kind autostart.Kind) error {
	switch kind {
	case autostart.KindSystemdUser, autostart.KindSystemdSystem:
		state, err := autostart.ServiceStatus(kind)
		if err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.service_state", state, kind))
		return nil
	default:
		pid, running := daemon.Running()
		if !running {
			console.Printf("%s\n", i18n.T("msg.not_running", kind))
			return nil
		}
		console.Printf("%s\n", i18n.T("msg.running_pid", pid, kind))
		return nil
	}
}
