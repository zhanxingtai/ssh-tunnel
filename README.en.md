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

- `key`: private key; `keyPath` points to the key file, and the passphrase is optional;
- `password`: password;
- `agent`: use the ssh-agent pointed to by `SSH_AUTH_SOCK`.

Passwords, private key texts and passphrases are **not kept in the configuration file**. They
live in the keyring of the system (Windows credential manager, macOS keychain, Linux secret
service); a machine without a usable keyring falls back to the encrypted file `secrets.enc`
next to the configuration file. See [Security notes](#security-notes).

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

Ways of giving a credential (the last two keep it out of the process list):

```bash
# 1) directly (it shows up in the process list)
ssh-tunnel config host add prod --host 10.0.0.10 --user root --password PASSWORD

# 2) from the standard input
echo -n PASSWORD | ssh-tunnel config host add prod --host 10.0.0.10 --user root --password -

# 3) from an environment variable
SSH_TUNNEL_PASSWORD=PASSWORD ssh-tunnel config host add prod \
  --host 10.0.0.10 --user root --password-env SSH_TUNNEL_PASSWORD
```

The passphrase works the same way: `--passphrase`, `--passphrase -`, `--passphrase-env <name>`.
`config show` lists the kinds of credential that are stored, never their values.

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

## macOS

- The archives are `ssh-tunnel-darwin-arm64.tar.gz` (Apple silicon) and
  `ssh-tunnel-darwin-x64.tar.gz` (Intel); unpack them and run `./ssh-tunnel`;
- The interface opens in the default browser (the same fallback as on Linux); the program
  itself ships no native window;
- The autostart uses a launchd login item: tick it on the settings page or run
  `ssh-tunnel autostart on`, which writes `~/Library/LaunchAgents/com.ssh-tunnel.plist` and
  starts `ssh-tunnel daemon` at login;
- The credentials live in the login keychain; the keychain may ask for permission the first
  time it is read.

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
| `config.json` | Configuration (without passwords and private keys) |
| `secrets.enc` / `secrets.key` | The encrypted credentials and their key, used by the fallback |
| `ssh-tunnel.log` | Runtime log |
| `ssh-tunnel.lock` | Single-instance lock (unix) |
| `runtime.json` | Running instance information (UI address, PID) |

On Windows this is `C:\Users\<user>\.config\ssh-tunnel\`.

## Security notes

- Passwords, private key texts and passphrases are kept in the keyring of the system (Windows
  credential manager, macOS keychain, Linux secret service), not in the configuration file. A
  machine without a usable keyring stores them in the AES-GCM encrypted file `secrets.enc` of
  the configuration directory (its key is `secrets.key`, mode 0600). The environment variable
  `SSH_TUNNEL_KEYRING=0` keeps every credential out of the keyring.
  The plain text credentials of the older configuration files are moved to the keyring and
  removed from the file when the program starts;
- The local interface of the graphical mode needs a session token (the address that opens the
  page carries a ticket that is valid for five minutes; the session token itself lives in a
  cookie of the browser) and checks the origin of the requests; the credentials themselves are
  never sent to the browser;
- On the command line, `--password -` (standard input) and `--password-env <name>`
  (environment variable) keep the password out of the process list;
- The configuration file has mode 0600 and holds only addresses, user names and key paths;
  prefer keys or ssh-agent;
- The first connection to an unknown host is written to `~/.ssh/known_hosts` (TOFU); later key
  changes are rejected;
- `insecureHostKey: true` skips verification; use it only for temporary testing;
- Remote port forwarding listens only on `127.0.0.1` by default; confirm the remote firewall
  policy before listening on `0.0.0.0`.

## License

This project is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.html).
