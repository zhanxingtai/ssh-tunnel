// Command ssh-tunnel manages SSH port forwards and SOCKS5 proxies with a
// terminal interface, a graphical configuration interface and, on Windows, a
// system tray icon.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ssh-tunnel/internal/autostart"
	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/console"
	"ssh-tunnel/internal/daemon"
	"ssh-tunnel/internal/gui"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/logging"
	"ssh-tunnel/internal/manager"
	"ssh-tunnel/internal/singleinstance"
	"ssh-tunnel/internal/tray"
	"ssh-tunnel/internal/version"
	"ssh-tunnel/internal/webviewui"
)

func main() {
	prepareConsole()
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errAlreadyRunning) {
			// The command handed over to the instance that is already
			// running; this is not a failure.
			return
		}
		console.Errorln(i18n.T("msg.error_line", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	// The language is a global option: it is accepted by every command and
	// may come before the command itself.
	args = splitLanguage(args)
	if langError != nil {
		return langError
	}

	cmd := "ui"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runTunnel(args)
	case "daemon":
		return runDaemon(args)
	case "ui", "gui":
		return runInterface(args)
	case "tray":
		return runTray(args)
	case "autostart":
		return runAutostart(args)
	case "config":
		return runConfigCLI(args)
	case "service":
		return runService(args)
	case "version":
		console.Println("ssh-tunnel", version.Version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return i18n.E("err.unknown_command", cmd)
	}
}

func usage() {
	console.Println(i18n.T("usage.text"))
}

// splitLanguage removes the --lang option from the arguments and applies it.
// The option is accepted everywhere, before or after the command.
func splitLanguage(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--lang" || arg == "-lang":
			if i+1 < len(args) {
				i++
				langOption = args[i]
			}
		case strings.HasPrefix(arg, "--lang="):
			langOption = strings.TrimPrefix(arg, "--lang=")
		default:
			out = append(out, arg)
		}
	}
	if langOption != "" {
		locale := i18n.Normalize(langOption)
		if locale == "" {
			langError = i18n.E("err.unknown_language", langOption)
			return out
		}
		i18n.Set(locale)
	}
	return out
}

// langOption holds the language given on the command line.
var langOption string

// langError holds the error of an unsupported language.
var langError error

// flags is a tiny argument parser for the global options.
type flags struct {
	configPath string
	addr       string
	noOpen     bool
	args       []string
}

func parseFlags(args []string) (flags, error) {
	f := flags{addr: "127.0.0.1:8787"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 >= len(args) {
				return f, i18n.E("err.flag_config_value")
			}
			i++
			f.configPath = args[i]
		case "--addr":
			if i+1 >= len(args) {
				return f, i18n.E("err.flag_addr_value")
			}
			i++
			f.addr = args[i]
		case "--no-open":
			f.noOpen = true
		default:
			f.args = append(f.args, args[i])
		}
	}
	return f, nil
}

// applyLanguage uses the language of the configuration. The language given on
// the command line wins over the one of the configuration.
func applyLanguage(cfg *config.Config) {
	if langOption != "" {
		return
	}
	i18n.Set(cfg.Settings.LanguageOrDetect())
}

// setup loads the configuration and prepares the manager.
func setup(configPath string) (*config.Config, *manager.Manager, *logging.Logger, string, error) {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return nil, nil, nil, "", err
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, "", err
	}
	applyLanguage(cfg)
	logPath := filepath.Join(filepath.Dir(configPath), "ssh-tunnel.log")
	log, err := logging.New(logPath)
	if err != nil {
		return nil, nil, nil, "", err
	}
	return cfg, manager.New(cfg, log), log, configPath, nil
}

func runTunnel(args []string) error {
	f, err := parseFlags(args)
	if err != nil {
		return err
	}
	if len(f.args) == 0 {
		return i18n.E("err.run_requires_tunnel")
	}
	cfg, mgr, log, _, err := setup(f.configPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	defer log.Close()

	name := f.args[0]
	tunnel, ok := cfg.TunnelByRef(name)
	if !ok {
		return i18n.E("err.tunnel_not_found", name)
	}
	name = tunnel.ID
	if err := mgr.Start(name); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.tunnel_started_ctrlc", name))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	events, cancel := mgr.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			console.Println("\n" + i18n.T("msg.stopping"))
			mgr.Stop(name)
			time.Sleep(200 * time.Millisecond)
			return nil
		case ev := <-events:
			console.Printf("[%s] %s %s\n", time.Now().Format("15:04:05"), ev.Status.Name, ev.Status.Detail)
		}
	}
}

// runInterface starts the local server and shows the management interface:
// in a native window when the build supports it, in the system browser
// otherwise.
func runInterface(args []string) error {
	f, err := parseFlags(args)
	if err != nil {
		return err
	}
	_, mgr, log, cfgPath, err := setup(f.configPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	defer log.Close()

	guard, err := acquireInstance(cfgPath)
	if err != nil {
		return err
	}
	defer guard.Release()

	server := gui.New(mgr, cfgPath)
	url, err := server.Listen(f.addr)
	if err != nil {
		return err
	}
	runtimeFile := gui.RuntimeFile(cfgPath)
	gui.WriteRuntimeFile(runtimeFile, gui.RuntimeInfo{
		URL: url, PID: os.Getpid(), StartedAt: time.Now(), Window: webviewui.Available(),
	})
	defer gui.RemoveRuntimeFile(runtimeFile)

	// 启动标记了自动启动的隧道。
	mgr.StartAuto()

	console.Println(i18n.T("msg.gui_address"), url)
	if f.noOpen {
		waitForSignal()
		return nil
	}
	if err := showInterface(url, mgr, log); err != nil {
		return err
	}
	return nil
}

// showInterface opens the management page in a native window and blocks
// until the window is closed. Without a native window the page is opened in
// the system browser and the process keeps running.
//
// A platform that has a native window never falls back to the browser: the
// browser would show the raw page of the local server instead of the
// program window.
//
// On Windows the window and the tray icon are one program: closing the
// window only hides it, the program stays in the notification area and the
// tray icon brings the window back.
func showInterface(url string, mgr *manager.Manager, log *logging.Logger) error {
	if webviewui.Available() {
		if tray.Available() {
			startTray(url, mgr, log)
		}
		defer watchShowRequests(url, log)()
		return webviewui.OpenWith(url, webviewui.Options{HideOnClose: true})
	}
	if err := gui.OpenBrowser(url); err != nil {
		console.Println(i18n.T("msg.open_manually"), url)
	}
	waitForSignal()
	return nil
}

// startTray starts the system tray in the background.
//
// The tray opens the interface, controls the tunnels and ends the program.
func startTray(url string, mgr *manager.Manager, log *logging.Logger) {
	go func() {
		if err := tray.Run(mgr, tray.Options{
			OpenGUI: func() {
				if err := webviewui.OpenOrShow(url); err != nil {
					log.Printf("cannot open the interface: %v", err)
				}
			},
			Quit: func() {
				webviewui.Close()
			},
		}); err != nil {
			log.Printf("the tray stopped: %v", err)
		}
	}()
}

// watchShowRequests lets a second instance ask the running one to show its
// window. The returned function stops the watching.
func watchShowRequests(url string, log *logging.Logger) func() {
	stop, err := singleinstance.WatchShow(showEventName, func() {
		if err := webviewui.OpenOrShow(url); err != nil {
			log.Printf("cannot open the interface: %v", err)
		}
	})
	if err != nil {
		return func() {}
	}
	return stop
}

// acquireInstance makes sure that only one background instance runs. When one
// is already running its interface is opened instead of starting a duplicate.
func acquireInstance(cfgPath string) (*singleinstance.Guard, error) {
	guard, err := singleinstance.Acquire(instanceMutexName, instanceLockPath(cfgPath))
	if err == nil {
		return guard, nil
	}
	if !errors.Is(err, singleinstance.ErrAlreadyRunning) {
		return nil, err
	}
	runtimeFile := gui.RuntimeFile(cfgPath)
	if info, ok := gui.ReadRuntimeFile(runtimeFile); ok && info.URL != "" {
		console.Println(i18n.T("msg.already_running_at"), info.URL)
		if info.Window {
			// The running instance has a window: ask it to show the
			// window instead of opening a second one.
			if singleinstance.SignalShow(showEventName) {
				return nil, errAlreadyRunning
			}
		} else {
			if err := gui.OpenBrowser(info.URL); err != nil {
				console.Println(i18n.T("msg.open_manually"), info.URL)
			}
		}
		return nil, errAlreadyRunning
	}
	return nil, fmt.Errorf("%w", singleinstance.ErrAlreadyRunning)
}

// errAlreadyRunning signals that the command handed over to a running
// instance and can exit successfully.
var errAlreadyRunning = errors.New("handed over to the running instance")

// instanceMutexName is the name of the Windows mutex.
const instanceMutexName = `Local\ssh-tunnel`

// showEventName is the name of the event a second instance uses to ask the
// running one to show its window.
const showEventName = `Local\ssh-tunnel-show`

// instanceLockPath returns the lock file used on unix-like systems.
func instanceLockPath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "ssh-tunnel.lock")
}

func runTray(args []string) error {
	f, err := parseFlags(args)
	if err != nil {
		return err
	}
	_, mgr, log, cfgPath, err := setup(f.configPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	defer log.Close()

	guard, err := acquireInstance(cfgPath)
	if err != nil {
		return err
	}
	defer guard.Release()

	server := gui.New(mgr, cfgPath)
	url, err := server.Listen(f.addr)
	if err != nil {
		return err
	}
	runtimeFile := gui.RuntimeFile(cfgPath)
	gui.WriteRuntimeFile(runtimeFile, gui.RuntimeInfo{URL: url, PID: os.Getpid(), StartedAt: time.Now(), Window: webviewui.Available()})
	defer gui.RemoveRuntimeFile(runtimeFile)

	// 启动标记了自动启动的隧道。
	mgr.StartAuto()

	console.Println(i18n.T("msg.gui_address"), url)

	// 别的实例可以让这个实例把窗口显示出来。
	defer watchShowRequests(url, log)()

	return tray.Run(mgr, tray.Options{
		OpenGUI: func() {
			// The window blocks until it is closed, so it runs in its
			// own goroutine to keep the tray menu responsive.
			if err := webviewui.OpenOrShow(url); err != nil {
				log.Printf("cannot open the interface: %v", err)
			}
		},
		Quit: func() {
			webviewui.Close()
		},
	})
}

// runAutostart manages the autostart entry. The mechanism can be given as
// the second argument; without it the one matching the environment is used.
func runAutostart(args []string) error {
	action := ""
	kind := autostart.DefaultKind()
	for _, arg := range args {
		switch arg {
		case "on", "off", "status":
			action = arg
		default:
			k, err := autostart.ParseKind(arg)
			if err != nil {
				return err
			}
			kind = k
		}
	}
	if action == "" {
		return i18n.E("err.autostart_requires_action", kindNames())
	}

	switch action {
	case "on":
		if err := autostart.Set(kind, true); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.autostart_enabled", kind))
		console.Println(i18n.T("msg.autostart_start_command"), autostart.Describe(kind))
		if kind == autostart.KindSystemdUser {
			console.Println(i18n.T("msg.linger_note", currentUserName()))
		}
	case "off":
		if err := autostart.Set(kind, false); err != nil {
			return err
		}
		console.Printf("%s\n", i18n.T("msg.autostart_disabled", kind))
	case "status":
		enabled, err := autostart.Enabled(kind)
		if err != nil {
			return err
		}
		if enabled {
			console.Printf("%s\n", i18n.T("msg.autostart_status_enabled", kind, autostart.Describe(kind)))
		} else {
			console.Printf("%s\n", i18n.T("msg.autostart_status_disabled", kind))
		}
	default:
		return i18n.E("err.unknown_autostart_action", action)
	}
	return nil
}

// kindNames lists the autostart mechanisms of the current platform.
func kindNames() []string {
	kinds := autostart.Kinds()
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	return names
}

// currentUserName returns the name of the user running the process.
func currentUserName() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

// runDaemon starts the tunnels and keeps them running until the process is
// asked to stop. It is the mode used by the autostart entries on machines
// without a desktop.
func runDaemon(args []string) error {
	f, err := parseFlags(args)
	if err != nil {
		return err
	}
	cfg, mgr, log, cfgPath, err := setup(f.configPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	defer log.Close()

	guard, err := acquireInstance(cfgPath)
	if err != nil {
		return err
	}
	defer guard.Release()
	if err := daemon.WritePID(); err != nil {
		return err
	}
	defer daemon.RemovePID()

	names := f.args
	startAll := false
	if len(names) > 0 && names[0] == "--all" {
		startAll = true
		names = names[1:]
	}
	if len(names) == 0 {
		for _, t := range cfg.Tunnels {
			// 没有指定隧道时只启动标记了自动启动的隧道；
			// --all 则启动全部隧道。
			if startAll || t.AutoStart {
				names = append(names, t.ID)
			}
		}
	}
	if len(names) == 0 {
		// 没有配置也可以常驻，等用户添加配置后再启动隧道。
		log.Printf("%s", i18n.T("msg.no_tunnel_configured"))
		console.Errorln(i18n.T("msg.no_tunnel_configured"))
	}
	for _, name := range names {
		if t, ok := cfg.TunnelByRef(name); ok {
			name = t.ID
		}
		if err := mgr.Start(name); err != nil {
			log.Printf("cannot start %s: %v", name, err)
			console.Errorf("%s\n", i18n.T("msg.cannot_start", name, err))
			continue
		}
		console.Printf("%s\n", i18n.T("msg.tunnel_started", name))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	events, cancel := mgr.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			console.Println("\n" + i18n.T("msg.stopping"))
			mgr.StopAll()
			time.Sleep(200 * time.Millisecond)
			return nil
		case ev := <-events:
			console.Printf("[%s] %s %s\n", time.Now().Format("15:04:05"), ev.Status.Name, ev.Status.Detail)
		}
	}
}

func waitForSignal() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
