package i18n

// catalogEn holds the English messages.
var catalogEn = catalog{
	// Common
	"usage.text": `ssh-tunnel — SSH port forwarding and SOCKS5 proxy manager

Usage:
  ssh-tunnel                     open the graphical interface (default)
  ssh-tunnel run <tunnel>        run a tunnel in the foreground
  ssh-tunnel daemon [--all] [tunnel...]  run tunnels in the background (no interface)
  ssh-tunnel tray               stay in the system tray (Windows)
  ssh-tunnel autostart <on|off|status> [mechanism]
  ssh-tunnel service <start|stop|restart|status> [mechanism]
  ssh-tunnel config <path|list|show|host|tunnel> ...
  ssh-tunnel version

Configuration:
  ssh-tunnel config list                    list every host and tunnel
  ssh-tunnel config show <name or id>       show a host or a tunnel (JSON)
  ssh-tunnel config host add [name] [opts]  add a host
  ssh-tunnel config host edit <name> [opts] change a host
  ssh-tunnel config host remove <name>      remove a host (and its tunnels)
  ssh-tunnel config tunnel add [name] [opts]  add a tunnel
  ssh-tunnel config tunnel edit <name> [opts] change a tunnel
  ssh-tunnel config tunnel remove <name>      remove a tunnel

Host options:
  --name <name> --host <host> --port <port> --user <user>
  --auth <key|password|agent> --key <private key path> --password <password>
  --passphrase <passphrase> --auto-reconnect --no-auto-reconnect
  --insecure-host-key

Tunnel options:
  --name <name> --host <host> --type <local|remote|socks5>
  --listen <listen address> --target <target address> --auto-start --no-auto-start

Tunnel types:
  local   local forward (remote port -> local)
  remote  remote forward (local port -> remote)
  socks5  dynamic proxy (SOCKS5)

Common options:
  --config <file>

Autostart mechanisms (Linux):
  desktop        XDG desktop autostart, needs a desktop session
  systemd-user   systemd user service, needs loginctl enable-linger to boot
  systemd-system systemd system service, needs administrator rights
  cron           the @reboot entry of crontab
Without a mechanism the one matching the environment is used.

Global options:
  --config <file>   configuration file (default: ~/.config/ssh-tunnel/config.json)
  --addr <address>  address the interface listens on (default 127.0.0.1:8787)
  --lang <locale>   language of the interface and of the output (zh-CN or en-US)
  --no-open         do not open a window or a browser`,

	"err.unknown_command":   "unknown command %q",
	"err.flag_config_value": "--config requires a value",
	"err.flag_addr_value":   "--addr requires a value",
	"err.unknown_language":  "unsupported language %q, available: zh-CN, en-US",
	"err.unknown_theme":     "unsupported theme %q, available: auto, dark, light",

	"msg.error_line":  "error: %v",
	"msg.lang_option": "--lang",

	// Main command
	"err.run_requires_tunnel":  "run requires a tunnel name",
	"err.tunnel_not_found":     "tunnel %q not found",
	"msg.tunnel_started_ctrlc": "tunnel %s started, press Ctrl+C to stop",
	"msg.stopping":             "stopping…",
	"msg.gui_address":          "graphical interface:",
	"msg.open_manually":        "open the URL manually:",
	"msg.already_running_at":   "already running at",
	"msg.no_tunnel_configured": "no tunnel configured, waiting for one to be added",
	"msg.cannot_start":         "cannot start %s: %v",
	"msg.tunnel_started":       "tunnel %s started",

	// Autostart
	"err.autostart_requires_action": "autostart requires on, off or status (mechanism: %v)",
	"err.unknown_autostart_action":  "unknown autostart action %q",
	"msg.autostart_enabled":         "autostart enabled (%s)",
	"msg.autostart_disabled":        "autostart disabled (%s)",
	"msg.autostart_start_command":   "start command:",
	"msg.linger_note":               "note: enable lingering so it starts before any login: loginctl enable-linger %s",
	"msg.autostart_status_enabled":  "enabled (%s): %s",
	"msg.autostart_status_disabled": "disabled (%s)",

	"msg.auto_start_marker": "  auto-start",
	"msg.dynamic_target":    " (dynamic)",
	// Configuration command line
	"err.config_requires_action":  "config requires one of: path, list, show, host, tunnel",
	"err.unknown_config_action":   "unknown config action %q",
	"msg.no_host_configured":      "no host configured",
	"err.show_requires_name":      "show requires a host or tunnel name",
	"err.neither_host_nor_tunnel": "%q is neither a host nor a tunnel",
	"err.host_requires_action":    "host requires one of: add, edit, remove",
	"err.unknown_host_action":     "unknown host action %q",
	"err.edit_requires_host":      "edit requires a host name or identifier",
	"err.host_exists":             "host %s already exists; edit the existing host instead",
	"err.host_not_found":          "host %q not found",
	"err.remove_requires_host":    "remove requires a host name or identifier",
	"msg.host_added":              "host %s added (%s)",
	"msg.host_updated":            "host %s updated (%s)",
	"msg.host_removed":            "host %s removed (%s)",
	"err.tunnel_requires_action":  "tunnel requires one of: add, edit, remove",
	"err.unknown_tunnel_action":   "unknown tunnel action %q",
	"err.edit_requires_tunnel":    "edit requires a tunnel name or identifier",
	"err.remove_requires_tunnel":  "remove requires a tunnel name or identifier",
	"err.host_option_required":    "--host is required",
	"msg.tunnel_added":            "tunnel %s added (%s)",
	"msg.tunnel_updated":          "tunnel %s updated (%s)",
	"msg.tunnel_removed":          "tunnel %s removed (%s)",

	// Service
	"err.service_requires_action": "service requires start, stop, restart or status",
	"err.unknown_service_action":  "unknown service action %q",
	"msg.service_started":         "service started (%s)",
	"msg.background_started":      "background instance started (%s)",
	"msg.service_stopped":         "service stopped (%s)",
	"msg.background_stopped":      "background instance stopped (%s)",
	"msg.service_state":           "service %s (%s)",
	"msg.not_running":             "not running (%s)",
	"msg.running_pid":             "running, pid %d (%s)",

	// Configuration validation
	"err.host_address_required": "host address is required",
	"err.port_out_of_range":     "port %d out of range",
	"err.ssh_user_required":     "ssh user is required",
	"err.password_required":     "password is required for password auth",
	"err.key_required":          "a private key path or the private key text is required for key auth",
	"err.key_path_required":     "private key path is required for key auth",
	"err.key_not_readable":      "private key %s is not readable: %w",
	"err.unknown_auth_type":     "unknown auth type %q",
	"err.tunnel_needs_host":     "tunnel needs a host",
	"err.unknown_tunnel_type":   "unknown tunnel type %q",
	"err.address_required":      "%s is required",
	"err.address_invalid":       "%s %q is invalid: %w",
	"err.address_missing_host":  "%s %q is missing a host",
	"err.address_invalid_port":  "%s %q has an invalid port",
	"err.host_prefix":           "host %s: %w",
	"err.tunnel_prefix":         "tunnel %s: %w",
	"err.unknown_host_ref":      "tunnel %s: unknown host %s",
	"field.private_key_text":    "private key text",
	"field.listen_address":      "listen address",
	"field.target_address":      "target address",

	// Configuration file
	"err.read_config":       "read config %s: %w",
	"err.parse_config":      "parse config %s: %w",
	"err.create_config_dir": "create config dir: %w",
	"err.encode_config":     "encode config: %w",
	"err.write_config":      "write config: %w",
	"err.replace_config":    "replace config: %w",

	// Tunnel supervision
	"err.manager_closed":         "manager is closed",
	"err.tunnel_already_running": "tunnel %q is already running",
	"err.tunnel_host_not_found":  "tunnel %q: host %q not found",
	"err.tunnel_invalid":         "tunnel %q is invalid: %w",
	"err.host_invalid":           "host %q is invalid: %w",
	"err.ssh_connection_closed":  "ssh connection closed",
	"detail.stopped":             "stopped",
	"detail.connecting":          "connecting",
	"detail.retry_in":            "retry in %s",
	"detail.connected_to":        "connected to %s",
	"detail.connection_lost":     "connection lost",
	"log.starting_tunnel":        "starting tunnel %s (%s) -> %s",
	"log.stopping_tunnel":        "stopping tunnel %s",
	"log.cannot_start":           "cannot start %s: %v",
	"log.connected":              "tunnel %s connected to %s",
	"log.lost":                   "tunnel %s lost: %v",
	"log.tunnel_error":           "tunnel %s: %v",

	// SSH transport
	"err.dial":                  "dial %s: %w",
	"err.handshake":             "ssh handshake with %s: %w",
	"err.ssh_dial":              "ssh dial %s: %w",
	"err.ssh_listen":            "ssh listen %s: %w",
	"err.ssh_password_required": "password is required",
	"err.ssh_key_required":      "private key path or text is required",
	"err.ssh_key_path_required": "private key path is required",
	"err.read_key":              "read private key %s: %w",
	"err.parse_key":             "parse private key %s: %w",
	"err.no_agent":              "no ssh agent socket (SSH_AUTH_SOCK) available",
	"err.connect_agent":         "connect to ssh agent %s: %w",
	"err.locate_home":           "locate home dir: %w",
	"err.read_known_hosts":      "read known_hosts %s: %w",
	"err.parse_known_hosts":     "parse known_hosts %s: %w",
	"err.append_known_hosts":    "append known_hosts: %w",
	"err.write_known_hosts":     "write known_hosts: %w",
	"log.keepalive_failed":      "keepalive failed: %v",

	"log.local_forward_listening":  "local forward %s -> %s listening",
	"log.local_forward_failed":     "local forward %s -> %s: %v",
	"log.remote_forward_listening": "remote forward %s -> %s listening",
	"log.remote_forward_failed":    "remote forward %s -> %s: %v",
	"log.socks5_listening":         "socks5 proxy listening on %s",
	"log.socks5_handshake_failed":  "socks5 handshake failed: %v",
	"log.socks5_request_failed":    "socks5 request failed: %v",
	"log.socks5_connect_failed":    "socks5 connect to %s failed: %v",

	"err.cannot_listen_text": "cannot listen on %s: %v",
	// Forwarding
	"err.cannot_listen":             "cannot listen on %s: %w",
	"err.unsupported_socks_version": "unsupported socks version %d",
	"err.unsupported_command":       "unsupported command %d",
	"err.unsupported_address_type":  "unsupported address type %d",

	"err.autostart_unsupported":           "autostart is not supported on this platform",
	"err.unknown_autostart_kind":          "unknown autostart kind %q, supported: %v",
	"err.unknown_autostart_kind_short":    "unknown autostart kind %q",
	"err.no_service_manager":              "%q has no service manager",
	"err.windows_no_service_manager":      "windows has no service manager, start %s directly",
	"err.windows_no_service_manager_stop": "windows has no service manager, stop %s from the tray",
	"err.instance_already_running":        "another instance is already running",
	"err.native_window_unavailable":       "native window is not available in this build",
	"err.webview_failed":                  "the webview window failed: %v",
	"err.webview_create":                  "cannot create the webview window",
	"err.create_log_dir":                  "create log dir: %w",
	"err.open_log_file":                   "open log file: %w",
	"err.locate_executable":               "locate executable: %w",

	"err.create_lock_dir":          "create lock dir: %w",
	"err.open_lock_file":           "open lock file %s: %w",
	"err.lock_file":                "lock %s: %w",
	"err.create_mutex":             "create mutex %s: %w",
	"err.open_run_key":             "open run key: %w",
	"err.read_autostart_value":     "read autostart value: %w",
	"err.delete_autostart_value":   "delete autostart value: %w",
	"err.write_autostart_value":    "write autostart value: %w",
	"err.no_service_manager_short": "windows has no service manager",
	"err.remove_desktop_entry":     "remove desktop entry: %w",
	"err.create_autostart_dir":     "create autostart dir: %w",
	"err.write_desktop_entry":      "write desktop entry: %w",
	"err.create_wants_dir":         "create wants dir: %w",
	"err.remove_old_link":          "remove old link: %w",
	"err.link_unit":                "link unit: %w",
	"err.create_unit_dir":          "create unit dir: %w",
	"err.write_unit_file":          "write unit file: %w",
	"err.write_crontab":            "write crontab: %v (%s)",
	"err.no_unit_dir":              "no unit directory",
	"err.systemctl":                "systemctl %s: %v (%s)",

	// Background instance
	"err.instance_running":     "the background instance is already running",
	"err.start_instance":       "start background instance: %w",
	"err.instance_exited":      "the background instance exited immediately (%v); another instance may be running",
	"err.instance_not_started": "the background instance did not start (pid %d)",
	"err.no_instance":          "no background instance recorded",
	"err.locate_process":       "locate process %d: %w",
	"err.stop_process":         "stop process %d: %w",
	"err.process_not_stopped":  "process %d did not stop",
	"err.create_state_dir":     "create state dir: %w",
	"err.write_pid_file":       "write pid file: %w",

	// Graphical interface
	"err.auth_failed":  "authentication failed: %w",
	"msg.connected_to": "connected to %s",
	"err.host_missing": "the host does not exist",

	"err.tray_unsupported":   "system tray is not supported on this platform",
	"err.tray_window_create": "cannot create the tray window",
	"err.tray_message_loop":  "the tray message loop failed",

	"autostart.desktop":         "Desktop autostart (XDG)",
	"autostart.systemd-user":    "systemd user service",
	"autostart.systemd-system":  "systemd system service",
	"autostart.cron":            "The @reboot entry of crontab",
	"autostart.registry":        "Windows startup entry",
	"field.keepalive":           "keepalive interval",
	"field.connect_timeout":     "connection timeout",
	"field.reconnect_delay":     "reconnect delay",
	"field.max_reconnect_delay": "maximum reconnect delay",
	"err.setting_out_of_range":  "%s must be between %d and %d",
	"err.setting_max_too_small": "%s must not be smaller than %s",
	"log.cannot_restart":        "cannot restart the tunnel %s: %v",

	// Tray
	"tray.tip":  "SSH-Tunnel · SSH tunnels and SOCKS5 proxies",
	"tray.open": "Open the interface",
	"tray.quit": "Quit",

	// Labels
	"type.local":    "Local forward",
	"type.remote":   "Remote forward",
	"type.socks5":   "SOCKS5 proxy",
	"auth.key":      "Key",
	"auth.password": "Password",
	"auth.agent":    "SSH Agent",
}
