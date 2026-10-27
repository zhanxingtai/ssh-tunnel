package manager

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/logging"
	"ssh-tunnel/internal/sshclient"
)

// fakeClient is a SSH client that never connects to anything: the forwarders
// only use it when a connection arrives.
type fakeClient struct {
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{done: make(chan struct{})}
}

func (c *fakeClient) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return nil, errors.New("the fake client does not dial")
}

func (c *fakeClient) Listen(network, addr string) (net.Listener, error) {
	return nil, errors.New("the fake client does not listen")
}

func (c *fakeClient) Done() <-chan struct{} { return c.done }

func (c *fakeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	return nil
}

// testConfig returns a configuration with one host and one tunnel.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Settings.ReconnectDelaySeconds = 1
	cfg.Settings.MaxReconnectDelaySeconds = 1
	host := config.NewHost()
	host.ID = "h1"
	host.Host = "127.0.0.1"
	host.Port = 22
	host.Auth = config.Auth{Type: config.AuthAgent, User: "root"}
	cfg.Hosts = append(cfg.Hosts, host)

	tunnel := config.NewTunnel(host.ID)
	tunnel.ID = "t1"
	tunnel.Type = config.ForwardLocal
	tunnel.Listen = freePort(t)
	tunnel.Target = "127.0.0.1:9"
	cfg.Tunnels = append(cfg.Tunnels, tunnel)
	return cfg
}

// freePort returns an address that is currently unused.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// newManager returns a manager with a fake SSH connection and a logger that
// writes into the temporary directory of the test.
func newManager(t *testing.T, cfg *config.Config, dial func(context.Context, sshclient.Options) (client, error)) *Manager {
	t.Helper()
	log, err := logging.New(filepath.Join(t.TempDir(), "ssh-tunnel.log"))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	mgr := New(cfg, log)
	mgr.dial = dial
	return mgr
}

// statusOfID returns the status of a tunnel of the configuration, even when
// the tunnel was never started.
func statusOfID(t *testing.T, mgr *Manager, id string) Status {
	t.Helper()
	for _, s := range mgr.Statuses() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("the status of %q is missing", id)
	return Status{}
}

// waitForState waits until the tunnel reaches one of the given states.
func waitForState(t *testing.T, mgr *Manager, id string, states ...State) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last State
	for time.Now().Before(deadline) {
		s, ok := mgr.StatusOf(id)
		if ok {
			last = s.State
			for _, want := range states {
				if s.State == want {
					return s.State
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the tunnel did not reach %v (last state %q)", states, last)
	return last
}

func TestStartConnects(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateConnected)

	status, _ := mgr.StatusOf("t1")
	if status.HostID != "h1" || status.Type != config.ForwardLocal {
		t.Errorf("unexpected status %+v", status)
	}
	if status.Uptime == "" {
		t.Errorf("the uptime is missing: %+v", status)
	}
}

func TestStartTwiceFails(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateConnected)
	if err := mgr.Start("t1"); err == nil {
		t.Fatal("starting a running tunnel has to fail")
	}
}

func TestStartUnknownTunnel(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("nope"); err == nil {
		t.Fatal("starting an unknown tunnel has to fail")
	}
}

func TestStop(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateConnected)
	mgr.Stop("t1")
	waitForState(t, mgr, "t1", StateStopped)
}

func TestFailedConnectionIsReported(t *testing.T) {
	cfg := testConfig(t)
	// The host does not reconnect on its own, so the tunnel stays in error.
	cfg.Hosts[0].AutoReconnect = false
	cfg.Settings.AutoReconnectDefault = false
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return nil, errors.New("no route to the host")
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateError)

	status, _ := mgr.StatusOf("t1")
	if status.LastError == "" {
		t.Errorf("the error is missing: %+v", status)
	}
}

func TestReconnectAfterAFailure(t *testing.T) {
	cfg := testConfig(t)
	var mu sync.Mutex
	attempts := 0
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return nil, errors.New("the first attempt fails")
		}
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateError, StateReconnecting)
	waitForState(t, mgr, "t1", StateConnected)

	status, _ := mgr.StatusOf("t1")
	if status.Reconnects == 0 {
		t.Errorf("the number of reconnects is missing: %+v", status)
	}
}

// TestTheConnectionLossIsNoticed checks that a broken connection brings the
// tunnel back into the reconnecting state.
func TestTheConnectionLossIsNoticed(t *testing.T) {
	cfg := testConfig(t)
	conn := newFakeClient()
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return conn, nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateConnected)

	// The server goes away.
	conn.Close()
	waitForState(t, mgr, "t1", StateReconnecting, StateError, StateConnected)
}

// TestRestartRunning checks that the running tunnels are started again, so
// that they use the new settings.
func TestRestartRunning(t *testing.T) {
	cfg := testConfig(t)
	var mu sync.Mutex
	dials := 0
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		return newFakeClient(), nil
	})
	defer mgr.Close()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForState(t, mgr, "t1", StateConnected)

	mgr.RestartRunning()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := dials
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	n := dials
	mu.Unlock()
	if n < 2 {
		t.Fatalf("the tunnel was not restarted: %d connections", n)
	}
	waitForState(t, mgr, "t1", StateConnected)
}

func TestStartAllAndStopAll(t *testing.T) {
	cfg := testConfig(t)
	second := cfg.Tunnels[0]
	second.ID = "t2"
	second.Listen = freePort(t)
	cfg.Tunnels = append(cfg.Tunnels, second)

	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	mgr.StartAll()
	waitForState(t, mgr, "t1", StateConnected)
	waitForState(t, mgr, "t2", StateConnected)

	mgr.StopAll()
	waitForState(t, mgr, "t1", StateStopped)
	waitForState(t, mgr, "t2", StateStopped)
}

func TestStartAutoOnlyStartsTheAutomaticTunnels(t *testing.T) {
	cfg := testConfig(t)
	cfg.Tunnels[0].AutoStart = false
	second := cfg.Tunnels[0]
	second.ID = "t2"
	second.Listen = freePort(t)
	second.AutoStart = true
	cfg.Tunnels = append(cfg.Tunnels, second)

	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	mgr.StartAuto()
	waitForState(t, mgr, "t2", StateConnected)

	status := statusOfID(t, mgr, "t1")
	if status.State != StateStopped {
		t.Errorf("the first tunnel is %q, it should stay stopped", status.State)
	}
}

func TestStatusesFollowTheConfiguration(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	statuses := mgr.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("unexpected number of statuses: %d", len(statuses))
	}
	if statuses[0].ID != "t1" || statuses[0].State != StateStopped {
		t.Errorf("unexpected status %+v", statuses[0])
	}
	if statuses[0].HostName == "" {
		t.Errorf("the name of the host is missing: %+v", statuses[0])
	}
}

// TestTheEventsArePublished checks that the subscribers see the changes of
// the state.
func TestTheEventsArePublished(t *testing.T) {
	cfg := testConfig(t)
	mgr := newManager(t, cfg, func(context.Context, sshclient.Options) (client, error) {
		return newFakeClient(), nil
	})
	defer mgr.Close()

	events, cancel := mgr.Subscribe()
	defer cancel()

	if err := mgr.Start("t1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.After(5 * time.Second)
	seen := map[State]bool{}
	for !seen[StateConnected] {
		select {
		case ev := <-events:
			seen[ev.Status.State] = true
		case <-deadline:
			t.Fatalf("the connected state was not published: %v", seen)
		}
	}
}
