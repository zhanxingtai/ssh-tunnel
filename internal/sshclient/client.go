// Package sshclient wraps the SSH transport used by the tunnels: dialing,
// authentication, host key checking and keepalives.
package sshclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/i18n"
)

// Options controls how a SSH connection is established.
type Options struct {
	Host     config.Host
	Settings config.Settings
	// KnownHostsPath overrides the default ~/.ssh/known_hosts location.
	KnownHostsPath string
	// AgentSocket overrides SSH_AUTH_SOCK.
	AgentSocket string
}

// Client is a connected SSH client with keepalive supervision.
type Client struct {
	client *ssh.Client

	keepalive time.Duration
	logf      func(format string, args ...any)

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
}

// Dial establishes a SSH connection according to the options.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	h := opts.Host
	if h.Port == 0 {
		h.Port = 22
	}
	addr := net.JoinHostPort(h.Host, fmt.Sprintf("%d", h.Port))

	hostKeyCB, err := hostKeyCallback(h, opts.KnownHostsPath)
	if err != nil {
		return nil, err
	}

	auth, err := authMethods(h.Auth, opts.AgentSocket)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(h.ConnectTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(opts.Settings.ConnectTimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	cfg := &ssh.ClientConfig{
		User:            h.Auth.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCB,
		Timeout:         timeout,
	}

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, i18n.E("err.dial", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, i18n.E("err.handshake", addr, err)
	}

	c := &Client{
		client:    ssh.NewClient(sshConn, chans, reqs),
		keepalive: time.Duration(h.KeepaliveSeconds) * time.Second,
		logf:      func(string, ...any) {},
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	if c.keepalive <= 0 {
		c.keepalive = time.Duration(opts.Settings.KeepaliveSeconds) * time.Second
	}
	c.startKeepalive()
	c.startWatch()
	return c, nil
}

// startWatch closes the done channel as soon as the connection dies.
func (c *Client) startWatch() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.client.Wait()
		close(c.done)
	}()
}

// Done is closed when the SSH connection is lost.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close tears the connection down.
func (c *Client) Close() error {
	var err error
	c.stopOnce.Do(func() {
		close(c.stop)
		// Close the connection first: the watcher goroutine only returns
		// once the connection is gone.
		err = c.client.Close()
		c.wg.Wait()
	})
	return err
}

// Dial opens a channel to addr through the SSH server.
func (c *Client) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := c.client.Dial(network, addr)
		ch <- result{conn, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, i18n.E("err.ssh_dial", addr, res.err)
		}
		return res.conn, nil
	}
}

// Listen asks the SSH server to listen on the given remote address.
func (c *Client) Listen(network, addr string) (net.Listener, error) {
	l, err := c.client.Listen(network, addr)
	if err != nil {
		return nil, i18n.E("err.ssh_listen", addr, err)
	}
	return l, nil
}

// Alive reports whether the underlying connection is still usable.
func (c *Client) Alive() bool {
	_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

func (c *Client) startKeepalive() {
	if c.keepalive <= 0 {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.keepalive)
		defer ticker.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-ticker.C:
				if _, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					c.logf("%s", i18n.T("log.keepalive_failed", err))
					c.client.Close()
					return
				}
			}
		}
	}()
}

func authMethods(a config.Auth, agentSocket string) ([]ssh.AuthMethod, error) {
	switch a.Type {
	case config.AuthPassword:
		if a.Password == "" {
			return nil, i18n.E("err.ssh_password_required")
		}
		return []ssh.AuthMethod{ssh.Password(a.Password)}, nil
	case config.AuthKey:
		data := []byte(strings.TrimSpace(a.KeyText))
		label := i18n.T("field.private_key_text")
		if len(data) == 0 {
			if a.KeyPath == "" {
				return nil, i18n.E("err.ssh_key_required")
			}
			var err error
			data, err = os.ReadFile(a.KeyPath)
			if err != nil {
				return nil, i18n.E("err.read_key", a.KeyPath, err)
			}
			label = a.KeyPath
		}
		var signer ssh.Signer
		var err error
		if a.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(a.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(data)
		}
		if err != nil {
			return nil, i18n.E("err.parse_key", label, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case config.AuthAgent:
		socket := agentSocket
		if socket == "" {
			socket = os.Getenv("SSH_AUTH_SOCK")
		}
		if socket == "" {
			return nil, i18n.E("err.no_agent")
		}
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, i18n.E("err.connect_agent", socket, err)
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, nil
	default:
		return nil, i18n.E("err.unknown_auth_type", a.Type)
	}
}

// hostKeyCallback verifies the server key against known_hosts. Unknown hosts
// are trusted on first use (TOFU) and appended to the file.
func hostKeyCallback(h config.Host, knownHostsPath string) (ssh.HostKeyCallback, error) {
	if h.InsecureHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	path := knownHostsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, i18n.E("err.locate_home", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return tofuCallback(path), nil
		}
		return nil, i18n.E("err.read_known_hosts", path, err)
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, i18n.E("err.parse_known_hosts", path, err)
	}
	// Unknown hosts are accepted once and recorded; a host whose key changed
	// is always rejected.
	var mu sync.Mutex
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			mu.Lock()
			defer mu.Unlock()
			return appendKnownHost(path, hostname, remote, key)
		}
		return err
	}, nil
}

// tofuCallback accepts an unknown host key once and records it.
func tofuCallback(path string) ssh.HostKeyCallback {
	var mu sync.Mutex
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		return appendKnownHost(path, hostname, remote, key)
	}
}

// appendKnownHost adds the key of an unknown host to the known_hosts file.
func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return i18n.E("err.append_known_hosts", err)
	}
	defer f.Close()
	host := hostname
	if remote != nil {
		host = remote.String()
	}
	line := knownhosts.Line([]string{host}, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return i18n.E("err.write_known_hosts", err)
	}
	return nil
}
