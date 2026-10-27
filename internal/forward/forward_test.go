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
