package forward

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"

	"ssh-tunnel/internal/i18n"
)

// SOCKS5 protocol constants.
const (
	socks5Version    = 0x05
	socks5NoAuth     = 0x00
	socks5CmdConnect = 0x01

	socks5AddrIPv4   = 0x01
	socks5AddrDomain = 0x03
	socks5AddrIPv6   = 0x04

	socks5ReplySuccess             = 0x00
	socks5ReplyGeneralFailure      = 0x01
	socks5ReplyHostUnreachable     = 0x04
	socks5ReplyConnectionRefused   = 0x05
	socks5ReplyCommandNotSupported = 0x07
	socks5ReplyAddressNotSupported = 0x08
)

// StartSOCKS5 runs a SOCKS5 proxy on the local machine. Every connection is
// established through the SSH server. When target is not empty every request
// is redirected to that fixed address instead of the requested one. The
// returned function releases the local address.
func StartSOCKS5(ctx context.Context, client SSH, listen, target string, logf LogFunc) (func(), error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, &BindError{Addr: listen, Err: err}
	}
	stop := closeOnce(ln)
	go func() {
		<-ctx.Done()
		stop()
	}()
	logf("%s", i18n.T("log.socks5_listening", listen))
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSocks5(ctx, client, conn, target, logf)
		}
	}()
	return stop, nil
}

func handleSocks5(ctx context.Context, client SSH, conn net.Conn, target string, logf LogFunc) {
	defer conn.Close()
	if err := socks5Handshake(conn); err != nil {
		logf("%s", i18n.T("log.socks5_handshake_failed", err))
		return
	}
	addr, err := socks5ReadRequest(conn)
	if err != nil {
		logf("%s", i18n.T("log.socks5_request_failed", err))
		return
	}
	if target != "" {
		addr = target
	}
	remote, err := client.Dial(ctx, "tcp", addr)
	if err != nil {
		logf("%s", i18n.T("log.socks5_connect_failed", addr, err))
		socks5Reply(conn, socks5ReplyHostUnreachable)
		return
	}
	defer remote.Close()
	if err := socks5Reply(conn, socks5ReplySuccess); err != nil {
		return
	}
	copyBridge(conn, remote)
}

// socks5Handshake performs the method negotiation.
func socks5Handshake(conn net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != socks5Version {
		return i18n.E("err.unsupported_socks_version", head[0])
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	for _, m := range methods {
		if m == socks5NoAuth {
			_, err := conn.Write([]byte{socks5Version, socks5NoAuth})
			return err
		}
	}
	_, err := conn.Write([]byte{socks5Version, 0xff})
	return err
}

// socks5ReadRequest reads a CONNECT request and returns the target address.
func socks5ReadRequest(conn net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", err
	}
	if head[0] != socks5Version {
		return "", i18n.E("err.unsupported_socks_version", head[0])
	}
	if head[1] != socks5CmdConnect {
		socks5Reply(conn, socks5ReplyCommandNotSupported)
		return "", i18n.E("err.unsupported_command", head[1])
	}
	var host string
	switch head[3] {
	case socks5AddrIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = net.IP(buf).String()
	case socks5AddrDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return "", err
		}
		buf := make([]byte, int(l[0]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = string(buf)
	case socks5AddrIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host = net.IP(buf).String()
	default:
		socks5Reply(conn, socks5ReplyAddressNotSupported)
		return "", i18n.E("err.unsupported_address_type", head[3])
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// socks5Reply sends a reply with a placeholder bound address.
func socks5Reply(conn net.Conn, code byte) error {
	_, err := conn.Write([]byte{socks5Version, code, 0x00, socks5AddrIPv4, 0, 0, 0, 0, 0, 0})
	return err
}
