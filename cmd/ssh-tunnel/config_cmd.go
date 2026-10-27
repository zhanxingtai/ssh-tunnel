package main

import (
	"encoding/json"
	"flag"
	"strings"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/console"
	"ssh-tunnel/internal/i18n"
)

// runConfigCLI handles the "config" subcommand: listing, showing, adding,
// editing and removing hosts and tunnels from the command line.
func runConfigCLI(args []string) error {
	args, globalPath = splitGlobalConfig(args)
	if len(args) == 0 {
		return i18n.E("err.config_requires_action")
	}
	switch args[0] {
	case "path":
		path, err := config.DefaultPath()
		if err != nil {
			return err
		}
		console.Println(path)
		return nil
	case "list":
		return configList(args[1:])
	case "show":
		return configShow(args[1:])
	case "host":
		return configHost(args[1:])
	case "tunnel":
		return configTunnel(args[1:])
	default:
		return i18n.E("err.unknown_config_action", args[0])
	}
}

// globalPath holds the configuration file given before the subcommand.
var globalPath string

// splitGlobalConfig removes a global --config option from the arguments and
// returns the remaining ones.
func splitGlobalConfig(args []string) ([]string, string) {
	path := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		default:
			out = append(out, args[i])
		}
	}
	return out, path
}

// configList prints the hosts and the tunnels of the configuration file.
func configList(args []string) error {
	path, err := configPath(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	if len(cfg.Hosts) == 0 {
		console.Println(i18n.T("msg.no_host_configured"))
		return nil
	}
	for _, h := range cfg.Hosts {
		console.Printf("%-8s %-20s %s@%s:%d  auth=%s\n", h.ID, h.DisplayName(), h.Auth.User, h.Host, h.Port, h.Auth.Type)
		for _, t := range cfg.TunnelsOfHost(h.ID) {
			auto := ""
			if t.AutoStart {
				auto = i18n.T("msg.auto_start_marker")
			}
			mapping := t.Listen + " → " + t.Target
			if t.Type == config.ForwardSocks5 && t.Target == "" {
				mapping = t.Listen + " →" + i18n.T("msg.dynamic_target")
			}
			console.Printf("  %-8s %-20s %-10s %s%s\n", t.ID, t.DisplayName(), config.TypeLabel(t.Type), mapping, auto)
		}
	}
	return nil
}

// configShow prints one host or one tunnel as JSON.
func configShow(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.show_requires_name")
	}
	path, err := configPath(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	var value any
	if h, ok := cfg.HostByRef(args[0]); ok {
		value = h
	} else if t, ok := cfg.TunnelByRef(args[0]); ok {
		value = t
	} else {
		return i18n.E("err.neither_host_nor_tunnel", args[0])
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	console.Println(string(data))
	return nil
}

// hostFlags holds the options of the host commands.
type hostFlags struct {
	name            string
	host            string
	port            int
	user            string
	auth            string
	keyPath         string
	password        string
	passphrase      string
	autoReconnect   bool
	noAutoReconnect bool
	insecureHostKey bool
	configFile      string
}

// register adds the host options to a flag set.
func (f *hostFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.name, "name", "", "显示名")
	fs.StringVar(&f.host, "host", "", "主机地址")
	fs.IntVar(&f.port, "port", 0, "SSH 端口")
	fs.StringVar(&f.user, "user", "", "SSH 用户")
	fs.StringVar(&f.auth, "auth", "", "认证方式：key / password / agent")
	fs.StringVar(&f.keyPath, "key", "", "私钥路径")
	fs.StringVar(&f.password, "password", "", "密码")
	fs.StringVar(&f.passphrase, "passphrase", "", "私钥口令")
	fs.BoolVar(&f.autoReconnect, "auto-reconnect", true, "断线自动重连")
	fs.BoolVar(&f.noAutoReconnect, "no-auto-reconnect", false, "关闭断线自动重连")
	fs.BoolVar(&f.insecureHostKey, "insecure-host-key", false, "跳过主机密钥校验")
	fs.StringVar(&f.configFile, "config", "", "配置文件路径")
}

// path returns the configuration file the command works on.
func (f *hostFlags) path() (string, error) {
	if f.configFile != "" {
		return f.configFile, nil
	}
	if globalPath != "" {
		return globalPath, nil
	}
	return config.DefaultPath()
}

// apply copies the given flags onto a host.
func (f *hostFlags) apply(h *config.Host, given flagSet) error {
	if given["name"] {
		h.Name = f.name
	}
	if given["host"] {
		h.Host = f.host
	}
	if given["port"] {
		h.Port = f.port
	}
	if given["user"] {
		h.Auth.User = f.user
	}
	if given["auth"] {
		h.Auth.Type = strings.ToLower(f.auth)
		switch h.Auth.Type {
		case config.AuthPassword:
			h.Auth.KeyPath = ""
			h.Auth.KeyText = ""
			h.Auth.Passphrase = ""
		case config.AuthKey:
			h.Auth.Password = ""
		}
	}
	if given["key"] {
		h.Auth.KeyPath = f.keyPath
	}
	if given["password"] {
		h.Auth.Password = f.password
	}
	if given["passphrase"] {
		h.Auth.Passphrase = f.passphrase
	}
	if given["auto-reconnect"] {
		h.AutoReconnect = f.autoReconnect
	}
	if given["no-auto-reconnect"] {
		h.AutoReconnect = false
	}
	if given["insecure-host-key"] {
		h.InsecureHostKey = f.insecureHostKey
	}
	return nil
}

// configHost handles the "config host" commands.
func configHost(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.host_requires_action")
	}
	switch args[0] {
	case "add":
		return configHostAdd(args[1:])
	case "edit":
		return configHostEdit(args[1:])
	case "remove":
		return configHostRemove(args[1:])
	default:
		return i18n.E("err.unknown_host_action", args[0])
	}
}

// configHostAdd creates a new host.
func configHostAdd(args []string) error {
	fs := flag.NewFlagSet("config host add", flag.ContinueOnError)
	var f hostFlags
	f.register(fs)
	flagArgs, positional := splitArgs(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	path, err := f.path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	h := config.NewHost()
	if err := f.apply(&h, givenFlags(fs)); err != nil {
		return err
	}
	// 显示名也可以作为位置参数给出。
	if len(positional) > 0 && !givenFlags(fs)["name"] {
		h.Name = positional[0]
	}
	if errs := config.ValidateHost(&h); len(errs) > 0 {
		return errs[0]
	}
	if cfg.HostExists(h) {
		return i18n.E("err.host_exists", h.UserAddress())
	}
	cfg.UpsertHost(h)
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.host_added", h.DisplayName(), path))
	return nil
}

// configHostEdit changes an existing host.
func configHostEdit(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.edit_requires_host")
	}
	fs := flag.NewFlagSet("config host edit", flag.ContinueOnError)
	var f hostFlags
	f.register(fs)
	flagArgs, positional := splitArgs(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) == 0 {
		return i18n.E("err.edit_requires_host")
	}
	ref := positional[0]
	path, err := f.path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	h, ok := cfg.HostByRef(ref)
	if !ok {
		return i18n.E("err.host_not_found", ref)
	}
	if err := f.apply(h, givenFlags(fs)); err != nil {
		return err
	}
	if errs := config.ValidateHost(h); len(errs) > 0 {
		return errs[0]
	}
	cfg.UpsertHost(*h)
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.host_updated", h.DisplayName(), path))
	return nil
}

// configHostRemove deletes a host and its tunnels.
func configHostRemove(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.remove_requires_host")
	}
	path, err := configPath(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	h, ok := cfg.HostByRef(args[0])
	if !ok {
		return i18n.E("err.host_not_found", args[0])
	}
	name := h.DisplayName()
	if !cfg.RemoveHost(h.ID) {
		return i18n.E("err.host_not_found", args[0])
	}
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.host_removed", name, path))
	return nil
}

// tunnelFlags holds the options of the tunnel commands.
type tunnelFlags struct {
	name        string
	host        string
	tunnelType  string
	listen      string
	target      string
	autoStart   bool
	noAutoStart bool
	configFile  string
}

// register adds the tunnel options to a flag set.
func (f *tunnelFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.name, "name", "", "显示名")
	fs.StringVar(&f.host, "host", "", "所属主机（名称或标识）")
	fs.StringVar(&f.tunnelType, "type", "", "类型：local / remote / socks5")
	fs.StringVar(&f.listen, "listen", "", "监听地址")
	fs.StringVar(&f.target, "target", "", "目标地址")
	fs.BoolVar(&f.autoStart, "auto-start", false, "自动启动")
	fs.BoolVar(&f.noAutoStart, "no-auto-start", false, "关闭自动启动")
	fs.StringVar(&f.configFile, "config", "", "配置文件路径")
}

// path returns the configuration file the command works on.
func (f *tunnelFlags) path() (string, error) {
	if f.configFile != "" {
		return f.configFile, nil
	}
	if globalPath != "" {
		return globalPath, nil
	}
	return config.DefaultPath()
}

// apply copies the given flags onto a tunnel.
func (f *tunnelFlags) apply(t *config.Tunnel, given flagSet) error {
	if given["name"] {
		t.Name = f.name
	}
	if given["host"] {
		t.HostID = f.host
	}
	if given["type"] {
		t.Type = strings.ToLower(f.tunnelType)
	}
	if given["listen"] {
		t.Listen = f.listen
	}
	if given["target"] {
		t.Target = f.target
	}
	if given["auto-start"] {
		t.AutoStart = f.autoStart
	}
	if given["no-auto-start"] {
		t.AutoStart = false
	}
	return nil
}

// configTunnel handles the "config tunnel" commands.
func configTunnel(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.tunnel_requires_action")
	}
	switch args[0] {
	case "add":
		return configTunnelAdd(args[1:])
	case "edit":
		return configTunnelEdit(args[1:])
	case "remove":
		return configTunnelRemove(args[1:])
	default:
		return i18n.E("err.unknown_tunnel_action", args[0])
	}
}

// configTunnelAdd creates a new tunnel.
func configTunnelAdd(args []string) error {
	fs := flag.NewFlagSet("config tunnel add", flag.ContinueOnError)
	var f tunnelFlags
	f.register(fs)
	flagArgs, positional := splitArgs(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	path, err := f.path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	t := config.NewTunnel("")
	if err := f.apply(&t, givenFlags(fs)); err != nil {
		return err
	}
	// 显示名也可以作为位置参数给出。
	if len(positional) > 0 && !givenFlags(fs)["name"] {
		t.Name = positional[0]
	}
	if t.HostID == "" {
		return i18n.E("err.host_option_required")
	}
	host, ok := cfg.HostByRef(t.HostID)
	if !ok {
		return i18n.E("err.host_not_found", t.HostID)
	}
	t.HostID = host.ID
	if errs := config.ValidateTunnel(&t); len(errs) > 0 {
		return errs[0]
	}
	cfg.UpsertTunnel(t)
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.tunnel_added", t.DisplayName(), path))
	return nil
}

// configTunnelEdit changes an existing tunnel.
func configTunnelEdit(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.edit_requires_tunnel")
	}
	fs := flag.NewFlagSet("config tunnel edit", flag.ContinueOnError)
	var f tunnelFlags
	f.register(fs)
	flagArgs, positional := splitArgs(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) == 0 {
		return i18n.E("err.edit_requires_tunnel")
	}
	ref := positional[0]
	path, err := f.path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	t, ok := cfg.TunnelByRef(ref)
	if !ok {
		return i18n.E("err.tunnel_not_found", ref)
	}
	if err := f.apply(t, givenFlags(fs)); err != nil {
		return err
	}
	if givenFlags(fs)["host"] {
		host, ok := cfg.HostByRef(t.HostID)
		if !ok {
			return i18n.E("err.host_not_found", t.HostID)
		}
		t.HostID = host.ID
	}
	if errs := config.ValidateTunnel(t); len(errs) > 0 {
		return errs[0]
	}
	cfg.UpsertTunnel(*t)
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.tunnel_updated", t.DisplayName(), path))
	return nil
}

// configTunnelRemove deletes a tunnel.
func configTunnelRemove(args []string) error {
	if len(args) == 0 {
		return i18n.E("err.remove_requires_tunnel")
	}
	path, err := configPath(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	applyLanguage(cfg)
	t, ok := cfg.TunnelByRef(args[0])
	if !ok {
		return i18n.E("err.tunnel_not_found", args[0])
	}
	name := t.DisplayName()
	if !cfg.RemoveTunnel(t.ID) {
		return i18n.E("err.tunnel_not_found", args[0])
	}
	if err := cfg.Save(path); err != nil {
		return err
	}
	console.Printf("%s\n", i18n.T("msg.tunnel_removed", name, path))
	return nil
}

// givenFlags returns the flags that were set on the command line.
func givenFlags(fs *flag.FlagSet) flagSet {
	given := flagSet{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	return given
}

// flagSet records the flags that were set on the command line.
type flagSet map[string]bool

// splitArgs separates the flags from the positional arguments. It allows a
// positional argument to be given before the options.
func splitArgs(fs *flag.FlagSet, args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) > 1 && arg[0] == '-' {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if idx := strings.Index(name, "="); idx >= 0 {
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	return flags, positional
}

// configPath returns the configuration file to work on.
func configPath(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 >= len(args) {
				return "", i18n.E("err.flag_config_value")
			}
			return args[i+1], nil
		}
	}
	if globalPath != "" {
		return globalPath, nil
	}
	return config.DefaultPath()
}
