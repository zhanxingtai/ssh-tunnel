# ssh-tunnel

**English** | [简体中文](README.md)

<img src="assets/banner.svg" alt="ssh-tunnel" width="720">

A Go-based SSH port-forwarding and SOCKS5 proxy manager with a native UI, system tray, and CLI.
It supports automatic reconnect, keepalive, host key verification, and autostart.

## Features

- Local / remote port forwarding and a SOCKS5 proxy;
- Live connection status, exponential-backoff reconnect, and a 30s keepalive by default;
- Native window (WebView2 on Windows), system tray, and single-instance mode;
- Simplified Chinese / English UI and CLI, with system / dark / light themes;
- Host key verification with TOFU; changed keys are rejected;
- Autostart via the Windows registry, or XDG / systemd / crontab on Linux.

## Installation

Requires Go 1.26 or newer.

```bash
git clone <repo> && cd ssh-tunnel
make build            # bin/ssh-tunnel
make build-windows    # bin/ssh-tunnel.exe
```

Or run `go build -o ssh-tunnel ./cmd/ssh-tunnel`.

Windows needs the WebView2 runtime. Native windows on Linux / macOS require cgo, gtk3, and
webkit2gtk-4.0; build them with `make build-webview`. The default build falls back to the
system browser when no native window is available.

## Quick start

### 1. Prepare authentication

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

### 2. Create a configuration

```bash
./ssh-tunnel
```

The default configuration is `~/.config/ssh-tunnel/config.json`; `ssh-tunnel config path`
prints the actual path. If the new location is empty, the legacy
`~/.ssh-tunnel/config.json` is copied there automatically.

Example:

```json
{
  "hosts": [
    {
      "id": "h1a2b3c4d",
      "name": "prod",
      "host": "10.0.0.10",
      "port": 22,
      "auth": { "type": "key", "user": "root", "keyPath": "~/.ssh/id_ed25519" }
    }
  ],
  "tunnels": [
    {
      "id": "t5e6f7a8b",
      "hostId": "h1a2b3c4d",
      "name": "website",
      "type": "local",
      "listen": "127.0.0.1:8080",
      "target": "127.0.0.1:80",
      "autoStart": true
    }
  ]
}
```

Hosts store addresses and credentials; tunnels store mappings. `autoStart` connects the tunnel
when the program starts. An empty `name` falls back to the address, or to the type plus the
listen address. Legacy `profiles` configurations are migrated automatically.

Tunnel types:

| Type | Meaning | `listen` | `target` |
| --- | --- | --- | --- |
| `local` | Local forwarding | Local listen address | Remote target address |
| `remote` | Remote forwarding | Remote listen address | Local target address |
| `socks5` | SOCKS5 proxy | Local listen address | Optional; empty for a dynamic proxy |

Authentication:

- `key`: private key; `keyPath` points to the key file, and `passphrase` is optional;
- `password`: password stored in plain text in the configuration file (mode 0600);
- `agent`: use the ssh-agent pointed to by `SSH_AUTH_SOCK`.

### 3. Usage

```bash
./ssh-tunnel                            # open the UI
./ssh-tunnel run website                # run one tunnel in the foreground
./ssh-tunnel daemon [tunnel...]         # run tunnels in the background
./ssh-tunnel tray                       # stay in the system tray (Windows)
./ssh-tunnel autostart on|off|status [method]
./ssh-tunnel service start|stop|restart|status [method]
./ssh-tunnel config list|show|host|tunnel
./ssh-tunnel config path
```

Global options: `--config <file>`, `--addr <address>`, `--no-open`, and
`--lang <zh-CN|en-US>`. `--config` may appear before or after the subcommand.

## Interface

The UI has **Tunnels**, **Logs**, and **Settings** tabs, supports Chinese / English and
system / dark / light themes, and shows the current version and website link in the header.
**Tunnels** is grouped by host and lets you expand, start, stop, edit, or delete tunnels;
**Logs** shows connection and forwarding events in real time.

**Settings** changes the language, theme, autostart, keepalive interval, connection timeout,
and reconnect options. Saving connection or reconnect settings restarts running tunnels once.
The same `user@host:port` cannot be added twice.

Windows has only one autostart method, so the method selector is hidden; Linux can choose
between XDG, systemd, and crontab.

## CLI management

```bash
ssh-tunnel config list
ssh-tunnel config show prod
ssh-tunnel config host add prod \
  --host 10.0.0.10 --port 22 --user deploy --auth key --key ~/.ssh/id_ed25519
ssh-tunnel config host edit prod --port 2200
ssh-tunnel config host remove prod

ssh-tunnel config tunnel add website \
  --host prod --type local --listen 127.0.0.1:8080 --target 127.0.0.1:80 --auto-start
ssh-tunnel config tunnel add proxy \
  --host prod --type socks5 --listen 127.0.0.1:1080
ssh-tunnel config tunnel edit website --target 127.0.0.1:8081
ssh-tunnel config tunnel remove website
```

Host options: `--name`, `--host`, `--port`, `--user`, `--auth`, `--key`, `--password`,
`--passphrase`, `--auto-reconnect`, `--no-auto-reconnect`, `--insecure-host-key`.

Tunnel options: `--name`, `--host`, `--type`, `--listen`, `--target`, `--auto-start`,
`--no-auto-start`.

Service management:

```bash
ssh-tunnel autostart on systemd-user
ssh-tunnel autostart status
ssh-tunnel autostart off systemd-user
ssh-tunnel service start|stop|restart|status
```

systemd modes are managed by systemd; cron and desktop modes start and stop the background
process directly, with the PID recorded in `~/.config/ssh-tunnel/daemon.pid`.

## Windows

- `make build-windows` produces `bin/ssh-tunnel.exe` and requires the WebView2 runtime;
- Running it opens the native window and keeps a tray icon; closing the window only minimizes
  to the tray, and **Exit** from the tray menu terminates the program;
- `ssh-tunnel.exe tray` stays in the tray without opening a window;
- Tray and window modes are single-instance;
- Enable autostart on the **Settings** page or with `ssh-tunnel autostart on`; it writes to
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.

## Linux autostart

Use XDG autostart when a desktop session is available. On headless machines, choose one of:

| Method | Location | Permission | Description |
| --- | --- | --- | --- |
| `systemd-user` | `~/.config/systemd/user/ssh-tunnel.service` | Regular user | Also run `loginctl enable-linger <user>` |
| `systemd-system` | `/etc/systemd/system/ssh-tunnel.service` | Administrator | System service with `User=` |
| `cron` | User crontab `@reboot` | Regular user | No dependencies |

If the method is omitted, it is selected from the environment. Autostart launches
`ssh-tunnel daemon`, which starts only tunnels marked for autostart and writes status changes
to the log.

## User data directory

All user data lives in `~/.config/ssh-tunnel/`:

| File | Description |
| --- | --- |
| `config.json` | Configuration |
| `ssh-tunnel.log` | Runtime log |
| `ssh-tunnel.lock` | Single-instance lock (unix) |
| `runtime.json` | Running instance information (UI address, PID) |

On Windows this is `C:\Users\<user>\.config\ssh-tunnel\`.

## Security notes

- Passwords in the configuration file are plain text and the file mode is 0600; prefer keys or
  ssh-agent;
- The first connection to an unknown host is written to `~/.ssh/known_hosts` (TOFU); later key
  changes are rejected;
- `insecureHostKey: true` skips verification; use it only for temporary testing;
- Remote port forwarding listens only on `127.0.0.1` by default; confirm the remote firewall
  policy before listening on `0.0.0.0`.

## License

This project is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.html).
