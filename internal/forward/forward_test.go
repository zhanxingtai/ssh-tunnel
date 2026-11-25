package forward

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSSH dials directly, standing in for a SSH server.
type fakeSSH struct{}

func (fakeSSH) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	return d.DialContext(ctx, network, addr)
}

func (fakeSSH) Listen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// freePort returns a port that is currently unused.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// dialWithRetry connects to addr, retrying while the listener starts.
func dialWithRetry(t *testing.T, addr string) net.Conn {
	t.Helper()
	var conn net.Conn
	var err error
	for i := 0; i < 100; i++ {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dial %s: %v", addr, err)
	return nil
}

func TestLocalForward(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from backend")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartLocalForward(ctx, fakeSSH{}, listen, backend.Listener.Addr().String(), func(string, ...any) {})
	if err != nil {
		t.Fatalf("start local forward: %v", err)
	}
	defer stop()

	// Wait for the listener to be ready.
	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + listen + "/")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("request through the tunnel failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from backend" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestIsLoopbackListen(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.2:8080", true},
		{"[::1]:8080", true},
		{"localhost:8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.10:8080", false},
		{"example.com:8080", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsLoopbackListen(c.addr); got != c.want {
			t.Errorf("IsLoopbackListen(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// recorder collects the messages of a forwarder.
type recorder struct {
	mu   sync.Mutex
	logs []string
}

func (r *recorder) logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func (r *recorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.logs, "\n")
}

// A remote forward that listens outside of the loopback interface only
// works when the SSH server honours the address, which depends on its
// GatewayPorts option. The user has to be told about it.
func TestRemoteForwardGatewayPortsHint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logs := &recorder{}
	stop, err := StartRemoteForward(ctx, fakeSSH{}, "0.0.0.0:0", "127.0.0.1:9", logs.logf)
	if err != nil {
		t.Fatalf("start remote forward: %v", err)
	}
	defer stop()

	if !strings.Contains(logs.text(), "GatewayPorts") {
		t.Fatalf("the GatewayPorts hint is missing from the log:\n%s", logs.text())
	}
}

// A remote forward that listens on the loopback interface needs no hint.
func TestRemoteForwardLoopbackNoHint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logs := &recorder{}
	stop, err := StartRemoteForward(ctx, fakeSSH{}, "127.0.0.1:0", "127.0.0.1:9", logs.logf)
	if err != nil {
		t.Fatalf("start remote forward: %v", err)
	}
	defer stop()

	if strings.Contains(logs.text(), "GatewayPorts") {
		t.Fatalf("the GatewayPorts hint should not be logged:\n%s", logs.text())
	}
}

func TestSOCKS5(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "proxied")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartSOCKS5(ctx, fakeSSH{}, listen, "", func(string, ...any) {})
	if err != nil {
		t.Fatalf("start socks5: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Method negotiation: version 5, one method (no authentication).
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("read greeting reply: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("unexpected greeting reply %v", reply)
	}

	host, portStr, err := net.SplitHostPort(backend.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split backend address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse backend port: %v", err)
	}
	ip := net.ParseIP(host).To4()
	request := []byte{0x05, 0x01, 0x00, 0x01}
	request = append(request, ip...)
	request = append(request, byte(port>>8), byte(port))

	if _, err := conn.Write(request); err != nil {
		t.Fatalf("write request: %v", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if head[1] != 0x00 {
		t.Fatalf("socks5 reply code %d", head[1])
	}
	// Skip the bound address.
	if _, err := io.ReadFull(conn, make([]byte, 6)); err != nil {
		t.Fatalf("read bound address: %v", err)
	}

	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "proxied") {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestSOCKS5FixedTarget(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fixed")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The client asks for an unreachable destination but the proxy must
	// connect to the configured target instead.
	stop, err := StartSOCKS5(ctx, fakeSSH{}, listen, backend.Listener.Addr().String(), func(string, ...any) {})
	if err != nil {
		t.Fatalf("start socks5: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	conn.Write([]byte{0x05, 0x01, 0x00})
	io.ReadFull(conn, make([]byte, 2))

	request := []byte{0x05, 0x01, 0x00, 0x01, 10, 0, 0, 1, 0x00, 0x50}
	conn.Write(request)
	head := make([]byte, 10)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if head[1] != 0x00 {
		t.Fatalf("socks5 reply code %d", head[1])
	}
	conn.Write([]byte("GET / HTTP/1.0\r\nHost: x\r\n\r\n"))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "fixed") {
		t.Fatalf("unexpected body %q", body)
	}
}

// TestHTTPConnect proxies a CONNECT request and forwards the tunnel data,
// including bytes sent in the same packet as the request.
func TestHTTPConnect(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "proxied http")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartHTTP(ctx, fakeSSH{}, listen, "", func(string, ...any) {})
	if err != nil {
		t.Fatalf("start http proxy: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// The request and the tunneled request go out together, so the proxy
	// must not lose bytes its reader buffered past the headers.
	req := "CONNECT " + backend.Listener.Addr().String() + " HTTP/1.1\r\n" +
		"Host: " + backend.Listener.Addr().String() + "\r\n\r\n" +
		"GET / HTTP/1.0\r\nHost: x\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read connect response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("connect status %d", resp.StatusCode)
	}
	resp2, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read tunneled response: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "proxied http") {
		t.Fatalf("unexpected body %q", body)
	}
}

// A fixed target overrides the address of the CONNECT request.
func TestHTTPFixedTarget(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "fixed http")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartHTTP(ctx, fakeSSH{}, listen, backend.Listener.Addr().String(), func(string, ...any) {})
	if err != nil {
		t.Fatalf("start http proxy: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := conn.Write([]byte("CONNECT 10.0.0.1:80 HTTP/1.1\r\n\r\nGET / HTTP/1.0\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read connect response: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("connect status %d", resp.StatusCode)
	}
	resp2, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read tunneled response: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "fixed http") {
		t.Fatalf("unexpected body %q", body)
	}
}

// A plain request with an absolute http:// URL is rewritten to origin form
// and proxied; the connection is closed once the response is relayed.
func TestHTTPPlainRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/page" {
			t.Errorf("backend path %q, want /page", r.URL.Path)
		}
		fmt.Fprint(w, "plain proxied")
	}))
	defer backend.Close()

	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartHTTP(ctx, fakeSSH{}, listen, "", func(string, ...any) {})
	if err != nil {
		t.Fatalf("start http proxy: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := "GET http://" + backend.Listener.Addr().String() + "/page HTTP/1.1\r\n" +
		"Host: " + backend.Listener.Addr().String() + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "plain proxied") {
		t.Fatalf("unexpected body %q", body)
	}
}

// A plain request for an https:// URL cannot be forwarded without
// terminating TLS; it is rejected.
func TestHTTPRejectsHttpsPlainRequest(t *testing.T) {
	listen := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := StartHTTP(ctx, fakeSSH{}, listen, "", func(string, ...any) {})
	if err != nil {
		t.Fatalf("start http proxy: %v", err)
	}
	defer stop()

	conn := dialWithRetry(t, listen)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := conn.Write([]byte("GET https://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != 405 {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}
