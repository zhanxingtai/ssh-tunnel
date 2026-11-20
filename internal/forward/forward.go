// Package forward implements the port forwarding primitives: local forwards,
// remote forwards and a SOCKS5 proxy.
package forward

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"

	"ssh-tunnel/internal/i18n"
)

// LogFunc is the logging callback used by the forwarders.
type LogFunc func(format string, args ...any)

// SSH is the subset of the SSH client used by the forwarders. It is
// implemented by *sshclient.Client and allows the forwarders to be tested
// without a real server.
type SSH interface {
	// Dial opens a channel to addr through the SSH server.
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
	// Listen asks the SSH server to listen on the given remote address.
	Listen(network, addr string) (net.Listener, error)
}

// BindError reports a local address that cannot be listened on. Retrying
// does not help in that case, so the supervisor stops instead of looping.
type BindError struct {
	Addr string
	Err  error
}

func (e *BindError) Error() string {
	return i18n.T("err.cannot_listen_text", e.Addr, e.Err)
}

func (e *BindError) Unwrap() error { return e.Err }

// copyBridge proxies data between two connections until either side closes.
func copyBridge(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		// Half close so the peer observes the EOF as well.
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}

// StartLocalForward listens on a local address and forwards every accepted
// connection to remote through the SSH server. remote is resolved by the SSH
// server. The returned function releases the local address; it must be
// called before the same address can be used again.
func StartLocalForward(ctx context.Context, client SSH, listen, remote string, logf LogFunc) (func(), error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, &BindError{Addr: listen, Err: err}
	}
	stop := closeOnce(ln)
	go func() {
		<-ctx.Done()
		stop()
	}()
	logf("%s", i18n.T("log.local_forward_listening", listen, remote))
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				remoteConn, err := client.Dial(ctx, "tcp", remote)
				if err != nil {
					logf("%s", i18n.T("log.local_forward_failed", listen, remote, err))
					return
				}
				defer remoteConn.Close()
				copyBridge(conn, remoteConn)
			}()
		}
	}()
	return stop, nil
}

// IsLoopbackListen reports whether a listen address stays on the loopback
// interface of the machine that listens.
//
// It matters for a remote forward: the SSH server binds the remote port
// itself. It only keeps the address the client asked for when its
// GatewayPorts option allows it, and binds the loopback address otherwise.
func IsLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// An address without a port is invalid; the whole value is
		// read as the host.
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// StartRemoteForward asks the SSH server to listen on remoteListen and
// connects every accepted connection to remote on this machine.
func StartRemoteForward(ctx context.Context, client SSH, remoteListen, remote string, logf LogFunc) (func(), error) {
	ln, err := client.Listen("tcp", remoteListen)
	if err != nil {
		return nil, i18n.E("err.cannot_listen", remoteListen, err)
	}
	stop := closeOnce(ln)
	go func() {
		<-ctx.Done()
		stop()
	}()
	logf("%s", i18n.T("log.remote_forward_listening", remoteListen, remote))
	if !IsLoopbackListen(remoteListen) {
		// The server may bind the remote port somewhere else than the
		// address that was asked for. Say so, otherwise the tunnel looks
		// broken without any clue about the reason.
		logf("%s", i18n.T("log.remote_forward_gateway_ports", remoteListen))
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				local, err := net.Dial("tcp", remote)
				if err != nil {
					logf("%s", i18n.T("log.remote_forward_failed", remoteListen, remote, err))
					return
				}
				defer local.Close()
				copyBridge(conn, local)
			}()
		}
	}()
	return stop, nil
}

// closeOnce returns a function that closes the listener exactly once.
func closeOnce(ln net.Listener) func() {
	var once sync.Once
	return func() {
		once.Do(func() { ln.Close() })
	}
}
