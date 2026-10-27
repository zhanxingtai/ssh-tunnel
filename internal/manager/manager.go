// Package manager supervises the tunnels: it keeps the SSH connection alive,
// restarts it after a failure and publishes status updates to the UI.
package manager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/forward"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/logging"
	"ssh-tunnel/internal/sshclient"
)

// State describes the lifecycle of a tunnel.
type State string

// Tunnel states.
const (
	StateStopped      State = "stopped"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
	StateError        State = "error"
)

// Status is a snapshot of one tunnel.
type Status struct {
	// ID is the identifier of the tunnel.
	ID string `json:"id"`
	// Name is the display name of the tunnel.
	Name string `json:"name"`
	// HostID is the host the tunnel is built on.
	HostID string `json:"hostId"`
	// HostName is the display name of that host.
	HostName string `json:"hostName,omitempty"`
	// Type is the kind of the tunnel.
	Type string `json:"type"`
	// Listen is the address the tunnel listens on.
	Listen string `json:"listen,omitempty"`
	// Target is the destination of the tunnel.
	Target string `json:"target,omitempty"`
	// AutoStart tells whether the tunnel starts with the application.
	AutoStart bool `json:"autoStart"`

	State          State     `json:"state"`
	Detail         string    `json:"detail,omitempty"`
	ConnectedSince time.Time `json:"connectedSince,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	Reconnects     int       `json:"reconnects"`
	Uptime         string    `json:"uptime,omitempty"`
}

// Event is published whenever the status of a tunnel changes.
type Event struct {
	Status Status `json:"status"`
}

// client is the part of the SSH client the manager needs. It hides the
// concrete client so that the state machine can be tested without a server.
type client interface {
	forward.SSH
	// Done is closed when the connection is lost.
	Done() <-chan struct{}
	// Close tears the connection down.
	Close() error
}

// Manager owns every tunnel of a configuration.
type Manager struct {
	mu      sync.RWMutex
	cfg     *config.Config
	log     *logging.Logger
	tunnels map[string]*tunnel
	subs    map[chan Event]struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	closed  bool
	// dial opens the SSH connection of a tunnel.
	dial func(ctx context.Context, opts sshclient.Options) (client, error)
}

// New creates a manager for the given configuration.
func New(cfg *config.Config, log *logging.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		cfg:     cfg,
		log:     log,
		tunnels: map[string]*tunnel{},
		subs:    map[chan Event]struct{}{},
		ctx:     ctx,
		cancel:  cancel,
		dial:    dialSSH,
	}
}

// dialSSH opens a real SSH connection.
func dialSSH(ctx context.Context, opts sshclient.Options) (client, error) {
	return sshclient.Dial(ctx, opts)
}

// Config returns the configuration used by the manager.
func (m *Manager) Config() *config.Config { return m.cfg }

// Log returns the logger used by the manager.
func (m *Manager) Log() *logging.Logger { return m.log }

// Subscribe returns a channel of status events and a cancel function.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 128)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

// Start launches the tunnel with the given identifier.
func (m *Manager) Start(id string) error {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()

	t, ok := cfg.TunnelByID(id)
	if !ok {
		return i18n.E("err.tunnel_not_found", id)
	}
	host, ok := cfg.HostByID(t.HostID)
	if !ok {
		return i18n.E("err.tunnel_host_not_found", id, t.HostID)
	}
	if errs := config.ValidateTunnel(t); len(errs) > 0 {
		return i18n.E("err.tunnel_invalid", id, errs[0])
	}
	if errs := config.ValidateHost(host); len(errs) > 0 {
		return i18n.E("err.host_invalid", host.DisplayName(), errs[0])
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return i18n.E("err.manager_closed")
	}
	if running, ok := m.tunnels[id]; ok && running.running() {
		m.mu.Unlock()
		return i18n.E("err.tunnel_already_running", id)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	tn := &tunnel{
		manager: m,
		config:  *t,
		host:    *host,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	m.tunnels[id] = tn
	m.mu.Unlock()

	tn.set(StateConnecting, "connecting")
	m.log.Printf("%s", i18n.T("log.starting_tunnel", t.DisplayName(), config.TypeLabel(t.Type), host.Address()))
	go tn.run(ctx)
	return nil
}

// Stop terminates the tunnel with the given identifier.
func (m *Manager) Stop(id string) {
	m.mu.RLock()
	t, ok := m.tunnels[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	t.cancel()
	m.log.Printf("%s", i18n.T("log.stopping_tunnel", t.config.DisplayName()))
}

// Restart stops the tunnel and starts it again, so that it picks up the
// current settings. It does nothing when the tunnel is not running.
func (m *Manager) Restart(id string) error {
	m.mu.RLock()
	t, ok := m.tunnels[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	if !t.running() {
		return nil
	}
	t.cancel()
	// The tunnel has to be gone before it can be started again: the
	// listener of the local address is only free once it returned.
	select {
	case <-t.done:
	case <-time.After(10 * time.Second):
		return i18n.E("err.restart_timeout")
	}
	return m.Start(id)
}

// RestartRunning restarts every running tunnel. It is used after the
// connection settings changed, so that they take effect right away.
func (m *Manager) RestartRunning() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.tunnels))
	for id, t := range m.tunnels {
		if t.running() {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		if err := m.Restart(id); err != nil {
			m.log.Printf("%s", i18n.T("log.cannot_restart", m.displayName(id), err))
		}
	}
}

// displayName returns the display name of a tunnel of the configuration.
func (m *Manager) displayName(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.cfg.TunnelByID(id); ok {
		return t.DisplayName()
	}
	return id
}

// Toggle starts the tunnel when it is stopped and stops it otherwise.
func (m *Manager) Toggle(id string) error {
	if s, ok := m.StatusOf(id); ok && s.State != StateStopped && s.State != StateError {
		m.Stop(id)
		return nil
	}
	return m.Start(id)
}

// StartAll starts every tunnel of the configuration.
func (m *Manager) StartAll() {
	for _, t := range m.tunnelsToStart() {
		if err := m.Start(t.ID); err != nil {
			m.log.Printf("%s", i18n.T("log.cannot_start", t.DisplayName(), err))
		}
	}
}

// StartAuto starts the tunnels that are marked to start automatically.
func (m *Manager) StartAuto() {
	for _, t := range m.tunnelsToStart() {
		if !t.AutoStart {
			continue
		}
		if err := m.Start(t.ID); err != nil {
			m.log.Printf("%s", i18n.T("log.cannot_start", t.DisplayName(), err))
		}
	}
}

// tunnelsToStart returns the tunnels of the configuration.
func (m *Manager) tunnelsToStart() []config.Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]config.Tunnel, 0, len(m.cfg.Tunnels))
	out = append(out, m.cfg.Tunnels...)
	return out
}

// StopAll stops every running tunnel.
func (m *Manager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.tunnels))
	for id := range m.tunnels {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// StatusOf returns the status of a single tunnel.
func (m *Manager) StatusOf(id string) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tunnels[id]
	if !ok {
		return Status{}, false
	}
	return t.status(), true
}

// Statuses returns the status of every tunnel, in configuration order.
func (m *Manager) Statuses() []Status {
	m.mu.RLock()
	cfg := m.cfg
	running := make(map[string]*tunnel, len(m.tunnels))
	for id, t := range m.tunnels {
		running[id] = t
	}
	m.mu.RUnlock()

	out := make([]Status, 0, len(cfg.Tunnels))
	for _, c := range cfg.Tunnels {
		var s Status
		if t, ok := running[c.ID]; ok {
			s = t.status()
		} else {
			s = Status{
				ID:        c.ID,
				Name:      c.DisplayName(),
				HostID:    c.HostID,
				Type:      c.Type,
				Listen:    c.Listen,
				Target:    c.Target,
				AutoStart: c.AutoStart,
				State:     StateStopped,
			}
		}
		if h, ok := cfg.HostByID(c.HostID); ok {
			s.HostName = h.DisplayName()
		}
		out = append(out, s)
	}
	return out
}

// Reload replaces the configuration used by the manager. Running tunnels keep
// their current configuration until they are restarted.
func (m *Manager) Reload(cfg *config.Config) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
}

// Close stops every tunnel and releases the manager.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	m.StopAll()
	m.cancel()
}

func (m *Manager) publish(st Status) {
	m.mu.RLock()
	subs := make([]chan Event, 0, len(m.subs))
	for ch := range m.subs {
		subs = append(subs, ch)
	}
	m.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- Event{Status: st}:
		default:
		}
	}
}

// tunnel is a single supervised mapping.
type tunnel struct {
	manager *Manager
	config  config.Tunnel
	host    config.Host

	mu         sync.Mutex
	state      State
	detail     string
	lastError  string
	since      time.Time
	reconnects int

	cancel context.CancelFunc
	done   chan struct{}
}

func (t *tunnel) running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state == StateConnecting || t.state == StateConnected || t.state == StateReconnecting
}

func (t *tunnel) status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Status{
		ID:             t.config.ID,
		Name:           t.config.DisplayName(),
		HostID:         t.config.HostID,
		Type:           t.config.Type,
		Listen:         t.config.Listen,
		Target:         t.config.Target,
		AutoStart:      t.config.AutoStart,
		State:          t.state,
		Detail:         t.detail,
		LastError:      t.lastError,
		Reconnects:     t.reconnects,
		ConnectedSince: t.since,
	}
	if !t.since.IsZero() && t.state == StateConnected {
		s.Uptime = time.Since(t.since).Truncate(time.Second).String()
	}
	return s
}

func (t *tunnel) set(state State, detail string) {
	t.mu.Lock()
	t.state = state
	t.detail = detail
	t.mu.Unlock()
	t.manager.publish(t.status())
}

func (t *tunnel) run(ctx context.Context) {
	defer close(t.done)
	m := t.manager
	settings := m.cfg.Settings
	delay := time.Duration(settings.ReconnectDelaySeconds) * time.Second
	if delay <= 0 {
		delay = 3 * time.Second
	}
	maxDelay := time.Duration(settings.MaxReconnectDelaySeconds) * time.Second
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	autoReconnect := t.host.AutoReconnect || settings.AutoReconnectDefault

	for {
		if ctx.Err() != nil {
			t.set(StateStopped, i18n.T("detail.stopped"))
			return
		}
		if t.reconnects > 0 {
			t.set(StateReconnecting, i18n.T("detail.retry_in", delay))
			select {
			case <-ctx.Done():
				t.set(StateStopped, i18n.T("detail.stopped"))
				return
			case <-time.After(delay):
			}
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}

		t.set(StateConnecting, i18n.T("detail.connecting"))
		conn, err := m.dial(ctx, sshclient.Options{
			Host:     t.host,
			Settings: settings,
		})
		if err != nil {
			t.mu.Lock()
			t.lastError = err.Error()
			t.mu.Unlock()
			t.set(StateError, err.Error())
			m.log.Printf("%s", i18n.T("log.tunnel_error", t.config.DisplayName(), err))
			if !autoReconnect {
				return
			}
			t.reconnects++
			continue
		}

		t.mu.Lock()
		t.since = time.Now()
		t.lastError = ""
		t.mu.Unlock()
		t.set(StateConnected, i18n.T("detail.connected_to", t.host.Address()))
		m.log.Printf("%s", i18n.T("log.connected", t.config.DisplayName(), t.host.Address()))

		err = t.runForward(ctx, conn)
		conn.Close()
		if ctx.Err() != nil {
			t.set(StateStopped, i18n.T("detail.stopped"))
			return
		}
		if err != nil {
			t.mu.Lock()
			t.lastError = err.Error()
			t.mu.Unlock()
			m.log.Printf("%s", i18n.T("log.lost", t.config.DisplayName(), err))
			// A local address that cannot be bound will not become
			// available by retrying: stop instead of looping.
			var bindErr *forward.BindError
			if errors.As(err, &bindErr) {
				t.set(StateError, err.Error())
				return
			}
		}
		t.set(StateError, i18n.T("detail.connection_lost"))
		if !autoReconnect {
			return
		}
		t.reconnects++
	}
}

// runForward starts the forwarder of the tunnel and blocks until it fails.
func (t *tunnel) runForward(ctx context.Context, conn client) error {
	m := t.manager
	logf := func(format string, args ...any) {
		m.log.Printf("[%s] %s", t.config.DisplayName(), fmt.Sprintf(format, args...))
	}

	var stop func()
	var err error
	switch t.config.Type {
	case config.ForwardLocal:
		stop, err = forward.StartLocalForward(ctx, conn, t.config.Listen, t.config.Target, logf)
	case config.ForwardRemote:
		stop, err = forward.StartRemoteForward(ctx, conn, t.config.Listen, t.config.Target, logf)
	case config.ForwardSocks5:
		stop, err = forward.StartSOCKS5(ctx, conn, t.config.Listen, t.config.Target, logf)
	default:
		return i18n.E("err.unknown_tunnel_type", t.config.Type)
	}
	if err != nil {
		return err
	}
	// The forwarder is released before this function returns so that the
	// local addresses are free again when the tunnel reconnects.
	defer stop()

	select {
	case <-ctx.Done():
		return nil
	case <-conn.Done():
		return i18n.E("err.ssh_connection_closed")
	}
}
