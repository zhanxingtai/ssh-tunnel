package forward

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"

	"ssh-tunnel/internal/i18n"
)

// StartHTTP runs an HTTP proxy on the local machine. Every connection is
// established through the SSH server. CONNECT requests are tunneled and plain
// requests with an absolute http:// URL are forwarded. When target is not
// empty every request is redirected to that fixed address instead of the
// requested one. The returned function releases the local address.
func StartHTTP(ctx context.Context, client SSH, listen, target string, logf LogFunc) (func(), error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, &BindError{Addr: listen, Err: err}
	}
	stop := closeOnce(ln)
	go func() {
		<-ctx.Done()
		stop()
	}()
	logf("%s", i18n.T("log.http_listening", listen))
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleHTTP(ctx, client, conn, target, logf)
		}
	}()
	return stop, nil
}

// httpResponse is a fixed HTTP response, closed right after it is sent.
func httpResponse(status, text string) string {
	return fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		status, len(text), text)
}

func handleHTTP(ctx context.Context, client SSH, conn net.Conn, target string, logf LogFunc) {
	defer conn.Close()
	br := bufio.NewReader(conn)

	req, err := http.ReadRequest(br)
	if err != nil {
		logf("%s", i18n.T("log.http_request_failed", err))
		conn.Write([]byte(httpResponse("400 Bad Request", "bad request\n")))
		return
	}
	if req.Method == http.MethodConnect {
		handleConnect(ctx, client, conn, br, req, target, logf)
		return
	}
	handlePlain(ctx, client, conn, br, req, target, logf)
}

// handleConnect tunnels a CONNECT request: it dials the requested address and
// bridges the connection after the 200 response.
func handleConnect(ctx context.Context, client SSH, conn net.Conn, br *bufio.Reader, req *http.Request, target string, logf LogFunc) {
	addr, err := connectTarget(req)
	if err != nil {
		logf("%s", i18n.T("log.http_request_failed", err))
		conn.Write([]byte(httpResponse("400 Bad Request", "bad CONNECT target\n")))
		return
	}
	if target != "" {
		addr = target
	}
	remote, err := client.Dial(ctx, "tcp", addr)
	if err != nil {
		logf("%s", i18n.T("log.http_connect_failed", addr, err))
		conn.Write([]byte(httpResponse("502 Bad Gateway", "connect failed\n")))
		return
	}
	defer remote.Close()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		return
	}
	// The reader may hold tunnel bytes the client sent right after the
	// request, so the bridge must keep reading from it.
	copyBridge(&bufferedConn{Conn: conn, r: br}, remote)
}

// connectTarget extracts the authority of a CONNECT request and returns it
// with the port filled in. Go parses the authority of a CONNECT request into
// different URL fields depending on its shape, so every candidate is tried.
func connectTarget(req *http.Request) (string, error) {
	authority := req.URL.Host
	if authority == "" {
		authority = req.Host
	}
	if authority == "" {
		authority = req.URL.Path
	}
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		host = authority
		port = "443"
	}
	if host == "" {
		return "", i18n.E("err.http_target_invalid", authority)
	}
	return net.JoinHostPort(host, port), nil
}

// handlePlain forwards one plain request whose URL is in absolute form
// (http://host/path). The URL is rewritten to origin form and the request is
// sent over a fresh connection to the target. The connection to the client is
// closed once the response has been relayed.
func handlePlain(ctx context.Context, client SSH, conn net.Conn, br *bufio.Reader, req *http.Request, target string, logf LogFunc) {
	defer req.Body.Close()
	u := req.URL
	if !u.IsAbs() || u.Scheme != "http" {
		// An https URL cannot be forwarded without terminating the TLS
		// session: the client has to use CONNECT instead.
		conn.Write([]byte(httpResponse("405 Method Not Allowed", "only http:// requests and CONNECT are supported\n")))
		return
	}
	host := u.Hostname()
	if host == "" {
		conn.Write([]byte(httpResponse("400 Bad Request", "bad request target\n")))
		return
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	addr := net.JoinHostPort(host, port)
	if target != "" {
		addr = target
	}
	remote, err := client.Dial(ctx, "tcp", addr)
	if err != nil {
		logf("%s", i18n.T("log.http_connect_failed", addr, err))
		conn.Write([]byte(httpResponse("502 Bad Gateway", "connect failed\n")))
		return
	}
	defer remote.Close()

	// Rewrite the request into a plain origin-form one for the target.
	u.Scheme = ""
	u.Host = ""
	req.Host = host
	if u.Port() != "" {
		req.Host = net.JoinHostPort(host, u.Port())
	}
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	req.Header.Set("Connection", "close")
	if err := req.Write(remote); err != nil {
		logf("%s", i18n.T("log.http_forward_failed", err))
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(remote), req)
	if err != nil {
		logf("%s", i18n.T("log.http_forward_failed", err))
		conn.Write([]byte(httpResponse("502 Bad Gateway", "no response from target\n")))
		return
	}
	defer resp.Body.Close()
	resp.Header.Del("Connection")
	resp.Close = true
	if err := resp.Write(conn); err != nil {
		return
	}
}

// bufferedConn exposes the buffered reader as a connection so that bytes
// already read from the underlying connection are not lost.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }
