// Package gui serves a local web interface used to configure the hosts and
// the tunnels on Windows, where a terminal is not always handy. The server
// only listens on the loopback interface.
package gui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"ssh-tunnel/internal/autostart"
	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/manager"
	"ssh-tunnel/internal/sshclient"
	"ssh-tunnel/internal/version"
)

//go:embed assets
var assets embed.FS

const (
	officialSite = "https://zhihuizhanxing.com/"
)

// Server exposes the configuration over HTTP.
type Server struct {
	mgr     *manager.Manager
	cfgPath string

	// token is the session token of this run of the program. It is given to
	// the browser in a cookie.
	token string
	// tickets holds the short-lived tickets that open the interface. The
	// address the program hands to the browser carries one of them, so that
	// the session token does not stay in the list of the processes of the
	// machine.
	tickets map[string]time.Time
	// host and port are the address the server listens on. The origin of a
	// request must be the one of the server.
	host     string
	port     string
	wildcard bool

	tokenMu sync.Mutex

	mu sync.Mutex
}

// New creates a GUI server.
//
// The token of the session is the fixed one of the configuration when there
// is one, so that the address of the interface stays the same over several
// runs. Without one the program makes a token up for this run.
func New(mgr *manager.Manager, cfgPath string) *Server {
	token := ""
	if value, err := mgr.Config().SessionToken(); err == nil {
		token = value
	}
	if token == "" {
		token = newToken()
	}
	return &Server{
		mgr:     mgr,
		cfgPath: cfgPath,
		token:   token,
		tickets: map[string]time.Time{},
	}
}

// newToken returns a random token.
func newToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// The token only has to be unguessable for the lifetime of the
		// process.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// newTicket returns a ticket that opens the interface for a short while.
func (s *Server) newTicket() string {
	ticket := newToken()
	s.tokenMu.Lock()
	s.tickets[ticket] = time.Now().Add(ticketLifetime)
	s.tokenMu.Unlock()
	return ticket
}

// ticketValid tells whether a ticket still opens the interface.
func (s *Server) ticketValid(ticket string) bool {
	if ticket == "" {
		return false
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	expiry, ok := s.tickets[ticket]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.tickets, ticket)
		return false
	}
	return true
}

// Handler builds the HTTP handler.
func (s *Server) Handler() (http.Handler, error) {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, fmt.Errorf("load assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", s.authorize(s.api))
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return s.withHeaders(s.withTokenCookie(mux)), nil
}

// withHeaders sets the headers that keep the page from leaking the address of
// the server, which carries the token of the session.
func (s *Server) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// Listen starts the server on the loopback interface and returns its URL.
//
// The address carries a short-lived ticket that opens the interface: the
// session token itself does not reach the command line of the browser.
func (s *Server) Listen(addr string) (string, error) {
	handler, err := s.Handler()
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen %s: %w", addr, err)
	}
	s.host, s.port, _ = net.SplitHostPort(ln.Addr().String())
	s.wildcard = s.host == "0.0.0.0" || s.host == "::"
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	return s.origin() + "/?token=" + s.newTicket(), nil
}

// SessionURL returns the address of the interface with the session token. It
// is used by the window and by the second instance, which asks the browser to
// open it.
func (s *Server) SessionURL() string {
	return s.origin() + "/?token=" + s.token
}

// origin returns the address of the server.
func (s *Server) origin() string {
	host := s.host
	if s.wildcard {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, s.port)
}

// The ways a request can carry the token of the session.
const (
	// tokenHeader is the header the page sends the token in.
	tokenHeader = "X-SSH-Tunnel-Token"
	// tokenCookie holds the token once the page was opened with it.
	tokenCookie = "ssh_tunnel_token"
	// ticketLifetime is how long a ticket that opens the interface stays
	// valid. The ticket is exchanged for the session token, which lives in a
	// cookie of the browser.
	ticketLifetime = 5 * time.Minute
)

// The reasons a request of the interface is refused. They are sent to the
// interface, so that it can tell a missing token from a request that comes
// from another site.
const (
	// forbiddenToken means that the request does not carry the token of the
	// session.
	forbiddenToken = "forbidden_token"
	// forbiddenOrigin means that the request does not come from the
	// interface itself.
	forbiddenOrigin = "forbidden_origin"
)

// authorize rejects the requests that do not come from the interface of this
// run of the program.
//
// A request must carry the token of the session, either in a header, in the
// address or in a cookie. A short-lived ticket is accepted as well: it is
// what the address the program opens carries.
//
// The origin of the request must be the one of the interface, so that a page
// of another site cannot drive the program.
func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.allowed(r) {
			s.writeForbidden(w, forbiddenToken)
			return
		}
		if !s.sameOrigin(r) {
			s.writeForbidden(w, forbiddenOrigin)
			return
		}
		next(w, r)
	}
}

// allowed tells whether the request carries the token of the session.
func (s *Server) allowed(r *http.Request) bool {
	candidates := []string{
		r.Header.Get(tokenHeader),
		r.URL.Query().Get("token"),
	}
	if cookie, err := r.Cookie(tokenCookie); err == nil {
		candidates = append(candidates, cookie.Value)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) == 1 {
			return true
		}
	}
	for _, candidate := range candidates {
		if s.ticketValid(candidate) {
			return true
		}
	}
	return false
}

// sameOrigin tells whether the request comes from the interface itself.
//
// The origin is compared with the address of the request: the browser sends
// both, and they are the same for a request that comes from the interface.
// They carry the local port of the tunnel when the interface is reached
// through one, which is not the port the server listens on.
//
// The host is only trusted when it is the address of the server or a loopback
// name: a page of another site could pretend to be the server.
func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// A request without an origin does not come from a browser.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return false
	}
	if !s.trustedHost(host) {
		return false
	}
	// The browser sends the address of the page in the host of the request.
	// A request that comes from the interface carries it in the origin as
	// well: the two are the same.
	if reqHost, reqPort, err := net.SplitHostPort(r.Host); err == nil {
		if strings.EqualFold(host, reqHost) && port == reqPort {
			return true
		}
	}
	// The port of the server is accepted as well: a client may send the
	// address of the server instead of the one of the page.
	return s.port != "" && port == s.port
}

// trustedHost tells whether the host of an origin may drive the program.
//
// The server trusts its own address and the loopback names: the interface is
// served on the loopback address of the machine when it is reached through a
// tunnel. Any other name is refused, because a page of another site could
// pretend to be the server.
func (s *Server) trustedHost(host string) bool {
	if s.wildcard {
		// The server listens on every address of the machine: the token is
		// the only defense left.
		return true
	}
	if s.host != "" && strings.EqualFold(host, s.host) {
		return true
	}
	// The loopback names always point to the server.
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// withTokenCookie gives the session token to the browser once, so that the
// page keeps it when its address changes.
//
// The token is only given to a request that proves it knows it.
func (s *Server) withTokenCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.provesToken(r) {
			http.SetCookie(w, &http.Cookie{
				Name:     tokenCookie,
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

// provesToken tells whether the request shows that it knows the session
// token: it either gives the token itself or a ticket that opens the
// interface.
func (s *Server) provesToken(r *http.Request) bool {
	candidates := []string{
		r.Header.Get(tokenHeader),
		r.URL.Query().Get("token"),
	}
	if cookie, err := r.Cookie(tokenCookie); err == nil {
		candidates = append(candidates, cookie.Value)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(s.token)) == 1 {
			return true
		}
		if s.ticketValid(candidate) {
			return true
		}
	}
	return false
}

// OpenBrowser opens the given URL with the default browser.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// openBrowser is replaced in tests so that they do not open a browser.
var openBrowser = OpenBrowser

// hostInput is the payload used to create or change a host.
type hostInput struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	Auth            hostAuthInput `json:"auth"`
	AutoReconnect   bool          `json:"autoReconnect"`
	InsecureHostKey bool          `json:"insecureHostKey"`
}

// hostAuthInput is the credentials sent by the interface.
//
// A credential that is left out keeps the stored value; a credential that is
// sent empty removes it. The interface never receives the stored values: it
// only tells whether they exist.
type hostAuthInput struct {
	Type       string  `json:"type"`
	User       string  `json:"user"`
	Password   *string `json:"password,omitempty"`
	KeyPath    string  `json:"keyPath,omitempty"`
	KeyText    *string `json:"keyText,omitempty"`
	Passphrase *string `json:"passphrase,omitempty"`
}

// authType returns the way of authentication of a request. A request that
// carries none keeps the one of the stored host.
func authType(in hostAuthInput, old *config.Host) string {
	if in.Type != "" {
		return in.Type
	}
	if old != nil && old.Auth.Type != "" {
		return old.Auth.Type
	}
	return config.AuthPassword
}

// resolveAuth completes the credentials sent by the interface with the
// values that are already stored.
//
// A credential that is left out keeps its stored value, a credential that is
// sent empty removes it. Changing the way of authentication removes the
// credentials that do not belong to it any more.
func resolveAuth(in hostAuthInput, old *config.Host) config.Auth {
	auth := config.Auth{Type: in.Type, User: in.User, KeyPath: in.KeyPath}
	var previous config.Auth
	changed := true
	if old != nil {
		previous = old.Auth
		changed = previous.Type != in.Type
	}

	auth.Password = previous.Password
	if in.Password != nil {
		auth.Password = *in.Password
	}
	if changed && in.Type != config.AuthPassword {
		auth.Password = ""
	}

	auth.KeyText = previous.KeyText
	if in.KeyText != nil {
		auth.KeyText = *in.KeyText
	}
	if changed && in.Type != config.AuthKey {
		auth.KeyText = ""
	}

	auth.Passphrase = previous.Passphrase
	if in.Passphrase != nil {
		auth.Passphrase = *in.Passphrase
	}
	if changed && in.Type != config.AuthKey {
		auth.Passphrase = ""
	}
	return auth
}

// hostView is the representation of a host sent to the interface.
//
// The credentials are never sent: the interface only learns whether they
// exist.
type hostView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	User            string `json:"user"`
	AuthType        string `json:"authType"`
	HasPassword     bool   `json:"hasPassword"`
	HasKeyText      bool   `json:"hasKeyText"`
	HasPassphrase   bool   `json:"hasPassphrase"`
	KeyPath         string `json:"keyPath,omitempty"`
	AutoReconnect   bool   `json:"autoReconnect"`
	InsecureHostKey bool   `json:"insecureHostKey"`
	// Counts holds the number of tunnels of every type of this host.
	Counts map[string]int `json:"counts"`
}

// tunnelView is the representation of a tunnel sent to the interface.
type tunnelView struct {
	config.Tunnel
	HostName string `json:"hostName,omitempty"`
	// Mapping is the mapping written the way the interface shows it.
	Mapping string `json:"mapping,omitempty"`
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	switch {
	case path == "hosts" && r.Method == http.MethodGet:
		s.writeJSON(w, s.hosts())
	case path == "hosts" && r.Method == http.MethodPost:
		s.saveHost(w, r)
	case path == "hosts/verify" && r.Method == http.MethodPost:
		s.verifyHost(w, r)
	case strings.HasPrefix(path, "hosts/") && r.Method == http.MethodDelete:
		s.deleteHost(w, strings.TrimPrefix(path, "hosts/"))
	case path == "tunnels" && r.Method == http.MethodGet:
		s.writeJSON(w, s.tunnels())
	case path == "tunnels" && r.Method == http.MethodPost:
		s.saveTunnel(w, r)
	case strings.HasPrefix(path, "tunnels/") && r.Method == http.MethodDelete:
		s.deleteTunnel(w, strings.TrimPrefix(path, "tunnels/"))
	case path == "status" && r.Method == http.MethodGet:
		s.writeJSON(w, s.mgr.Statuses())
	case path == "info" && r.Method == http.MethodGet:
		s.writeJSON(w, s.info())
	case path == "open-website" && r.Method == http.MethodPost:
		if err := openBrowser(officialSite); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, map[string]string{"result": "ok"})
	case path == "settings" && r.Method == http.MethodGet:
		s.writeJSON(w, s.settings())
	case path == "settings" && r.Method == http.MethodPost:
		s.saveSettings(w, r)
	case path == "autostart" && r.Method == http.MethodPost:
		s.saveAutostart(w, r)
	case path == "logs" && r.Method == http.MethodGet:
		s.writeJSON(w, s.mgr.Log().Lines())
	case strings.HasPrefix(path, "start/") && r.Method == http.MethodPost:
		s.control(w, strings.TrimPrefix(path, "start/"), true)
	case strings.HasPrefix(path, "stop/") && r.Method == http.MethodPost:
		s.control(w, strings.TrimPrefix(path, "stop/"), false)
	case path == "start-all" && r.Method == http.MethodPost:
		s.mgr.StartAll()
		s.writeJSON(w, map[string]string{"result": "ok"})
	case path == "stop-all" && r.Method == http.MethodPost:
		s.mgr.StopAll()
		s.writeJSON(w, map[string]string{"result": "ok"})
	case path == "reload" && r.Method == http.MethodPost:
		cfg, err := config.Load(s.cfgPath)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := cfg.CredentialError(); err != nil {
			s.mgr.Log().Printf("%s", err)
		}
		s.mgr.Reload(cfg)
		s.writeJSON(w, map[string]string{"result": "ok"})
	default:
		s.writeError(w, http.StatusNotFound, fmt.Errorf("unknown endpoint %s", path))
	}
}

// info describes the program to the interface: its version, where the
// configuration file lives, which language is used and which languages can be
// chosen.
func (s *Server) info() map[string]any {
	locales := make([]map[string]string, 0, len(i18n.Locales()))
	for _, l := range i18n.Locales() {
		locales = append(locales, map[string]string{"id": l, "name": i18n.Name(l)})
	}
	return map[string]any{
		"version":    version.Version,
		"configPath": s.cfgPath,
		"language":   i18n.Get(),
		"locales":    locales,
		"theme":      s.mgr.Config().Settings.ThemeOrAuto(),
	}
}

// settingsView holds the settings of the program shown in the interface.
type settingsView struct {
	// Language is the locale of the interface and of the messages.
	Language string `json:"language"`
	// Theme is the color scheme of the interface.
	Theme string `json:"theme"`
	// KeepaliveSeconds is the interval of the SSH keepalive probes.
	KeepaliveSeconds int `json:"keepaliveSeconds"`
	// ConnectTimeoutSeconds bounds the SSH handshake.
	ConnectTimeoutSeconds int `json:"connectTimeoutSeconds"`
	// ReconnectDelaySeconds is the first delay between two attempts.
	ReconnectDelaySeconds int `json:"reconnectDelaySeconds"`
	// MaxReconnectDelaySeconds is the longest delay between two attempts.
	MaxReconnectDelaySeconds int `json:"maxReconnectDelaySeconds"`
	// AutoReconnectDefault is used for the hosts that do not override it.
	AutoReconnectDefault bool `json:"autoReconnectDefault"`
	// Addr is the address the management interface listens on.
	Addr string `json:"addr"`
	// HasSessionToken tells whether a fixed token is stored.
	HasSessionToken bool `json:"hasSessionToken"`
}

// mechanismView describes one way of starting the program with the system.
type mechanismView struct {
	// Kind is the name of the mechanism, "systemd-user" for instance.
	Kind string `json:"kind"`
	// Name is the name of the mechanism in the language in use.
	Name string `json:"name"`
	// Enabled tells whether the entry exists.
	Enabled bool `json:"enabled"`
	// Description is the command used to start the program.
	Description string `json:"description"`
}

// autostartView describes the autostart entries of this machine.
type autostartView struct {
	// DefaultKind is the mechanism that fits the environment best.
	DefaultKind string `json:"defaultKind"`
	// Mechanisms lists every way of starting the program on this platform.
	Mechanisms []mechanismView `json:"mechanisms"`
}

// settings returns the settings of the program and the state of the
// autostart entries.
func (s *Server) settings() map[string]any {
	cfg := s.mgr.Config()
	hasToken := false
	if value, err := cfg.SessionToken(); err == nil && value != "" {
		hasToken = true
	}
	return map[string]any{
		"settings": settingsView{
			Language:                 s.mgr.Config().Settings.LanguageOrDetect(),
			Theme:                    cfg.Settings.ThemeOrAuto(),
			KeepaliveSeconds:         cfg.Settings.KeepaliveSeconds,
			ConnectTimeoutSeconds:    cfg.Settings.ConnectTimeoutSeconds,
			ReconnectDelaySeconds:    cfg.Settings.ReconnectDelaySeconds,
			MaxReconnectDelaySeconds: cfg.Settings.MaxReconnectDelaySeconds,
			AutoReconnectDefault:     cfg.Settings.AutoReconnectDefault,
			Addr:                     cfg.Settings.Addr,
			HasSessionToken:          hasToken,
		},
		"autostart": s.autostart(),
	}
}

// autostart returns the autostart entries of this machine.
func (s *Server) autostart() autostartView {
	view := autostartView{
		DefaultKind: string(autostart.DefaultKind()),
		Mechanisms:  make([]mechanismView, 0),
	}
	for _, kind := range autostart.Kinds() {
		enabled, err := autostart.Enabled(kind)
		if err != nil {
			// A mechanism that cannot be read is shown as switched off.
			enabled = false
		}
		view.Mechanisms = append(view.Mechanisms, mechanismView{
			Kind:        string(kind),
			Name:        i18n.T("autostart." + string(kind)),
			Enabled:     enabled,
			Description: autostart.Command(kind),
		})
	}
	return view
}

// settingsInput is the payload of the settings endpoint. A field that is
// left out keeps its current value, so that a single setting can be changed
// without sending all of them.
type settingsInput struct {
	// Language is the locale to use, "zh-CN" or "en-US".
	Language *string `json:"language"`
	// Theme is the color scheme to use, "auto", "dark" or "light".
	Theme *string `json:"theme"`
	// KeepaliveSeconds is the interval of the SSH keepalive probes.
	KeepaliveSeconds *int `json:"keepaliveSeconds"`
	// ConnectTimeoutSeconds bounds the SSH handshake.
	ConnectTimeoutSeconds *int `json:"connectTimeoutSeconds"`
	// ReconnectDelaySeconds is the first delay between two attempts.
	ReconnectDelaySeconds *int `json:"reconnectDelaySeconds"`
	// MaxReconnectDelaySeconds is the longest delay between two attempts.
	MaxReconnectDelaySeconds *int `json:"maxReconnectDelaySeconds"`
	// AutoReconnectDefault is used for the hosts that do not override it.
	AutoReconnectDefault *bool `json:"autoReconnectDefault"`
	// Addr is the address the management interface listens on. An empty
	// value means that the default one is used and that the daemon opens no
	// interface.
	Addr *string `json:"addr"`
	// SessionToken is the fixed token of the interface. An empty value
	// leaves the stored one alone.
	SessionToken *string `json:"sessionToken"`
	// ClearSessionToken removes the fixed token.
	ClearSessionToken bool `json:"clearSessionToken"`
}

// saveSettings changes the settings of the program.
//
// The connection settings only reach the running tunnels when they are
// restarted, so they are restarted right away.
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	cfg := s.mgr.Config()
	updated := cfg.Settings
	if in.Language != nil {
		locale := i18n.Normalize(*in.Language)
		if locale == "" {
			s.mu.Unlock()
			s.writeError(w, http.StatusBadRequest, i18n.E("err.unknown_language", *in.Language))
			return
		}
		updated.Language = locale
	}
	if in.Theme != nil {
		theme := config.NormalizeTheme(*in.Theme)
		if theme == "" {
			s.mu.Unlock()
			s.writeError(w, http.StatusBadRequest, i18n.E("err.unknown_theme", *in.Theme))
			return
		}
		updated.Theme = theme
	}
	if in.KeepaliveSeconds != nil {
		updated.KeepaliveSeconds = *in.KeepaliveSeconds
	}
	if in.ConnectTimeoutSeconds != nil {
		updated.ConnectTimeoutSeconds = *in.ConnectTimeoutSeconds
	}
	if in.ReconnectDelaySeconds != nil {
		updated.ReconnectDelaySeconds = *in.ReconnectDelaySeconds
	}
	if in.MaxReconnectDelaySeconds != nil {
		updated.MaxReconnectDelaySeconds = *in.MaxReconnectDelaySeconds
	}
	if in.AutoReconnectDefault != nil {
		updated.AutoReconnectDefault = *in.AutoReconnectDefault
	}
	if in.Addr != nil {
		updated.Addr = strings.TrimSpace(*in.Addr)
	}
	if err := validateSettings(&updated); err != nil {
		s.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	// A copy of the configuration is saved: the manager keeps using the
	// configuration it has until it is replaced.
	next := *cfg
	next.Settings = updated
	err := next.Save(s.cfgPath)
	s.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	// The fixed token lives in the secret store: it is not part of the
	// configuration file.
	if in.ClearSessionToken {
		if err := cfg.SetSessionToken(""); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else if in.SessionToken != nil && strings.TrimSpace(*in.SessionToken) != "" {
		token := strings.TrimSpace(*in.SessionToken)
		if err := config.ValidateSessionToken(token); err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := cfg.SetSessionToken(token); err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	// The language of the messages follows the choice immediately. A request
	// without a language leaves it alone: the language of the command line
	// wins over the one of the configuration.
	if in.Language != nil {
		i18n.Set(updated.Language)
	}
	s.mgr.Reload(&next)
	// The tunnels keep the settings they were started with, so they are
	// restarted when the connection settings changed.
	if connectionSettingsChanged(cfg.Settings, next.Settings) {
		s.mgr.RestartRunning()
	}
	s.writeJSON(w, map[string]any{
		"result":   "ok",
		"language": i18n.Get(),
		"theme":    updated.ThemeOrAuto(),
	})
}

// validateSettings checks the values of the settings.
func validateSettings(in *config.Settings) error {
	if in.KeepaliveSeconds < 1 || in.KeepaliveSeconds > 3600 {
		return i18n.E("err.setting_out_of_range", i18n.T("field.keepalive"), 1, 3600)
	}
	if in.ConnectTimeoutSeconds < 1 || in.ConnectTimeoutSeconds > 600 {
		return i18n.E("err.setting_out_of_range", i18n.T("field.connect_timeout"), 1, 600)
	}
	if in.ReconnectDelaySeconds < 1 || in.ReconnectDelaySeconds > 3600 {
		return i18n.E("err.setting_out_of_range", i18n.T("field.reconnect_delay"), 1, 3600)
	}
	if in.MaxReconnectDelaySeconds < 1 || in.MaxReconnectDelaySeconds > 86400 {
		return i18n.E("err.setting_out_of_range", i18n.T("field.max_reconnect_delay"), 1, 86400)
	}
	if err := config.ValidateInterfaceAddr(in.Addr); err != nil {
		return err
	}
	if in.MaxReconnectDelaySeconds < in.ReconnectDelaySeconds {
		return i18n.E("err.setting_max_too_small", i18n.T("field.max_reconnect_delay"), i18n.T("field.reconnect_delay"))
	}
	return nil
}

// connectionSettingsChanged reports whether the settings that only reach a
// tunnel when it is restarted changed.
func connectionSettingsChanged(old, updated config.Settings) bool {
	return old.KeepaliveSeconds != updated.KeepaliveSeconds ||
		old.ConnectTimeoutSeconds != updated.ConnectTimeoutSeconds ||
		old.ReconnectDelaySeconds != updated.ReconnectDelaySeconds ||
		old.MaxReconnectDelaySeconds != updated.MaxReconnectDelaySeconds ||
		old.AutoReconnectDefault != updated.AutoReconnectDefault
}

// autostartInput is the payload of the autostart endpoint.
type autostartInput struct {
	// Kind is the mechanism to change.
	Kind string `json:"kind"`
	// Enabled tells whether the entry has to exist.
	Enabled bool `json:"enabled"`
}

// saveAutostart registers or removes the autostart entry of a mechanism.
func (s *Server) saveAutostart(w http.ResponseWriter, r *http.Request) {
	var in autostartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	kind, err := autostart.ParseKind(in.Kind)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := autostart.Set(kind, in.Enabled); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, map[string]any{"result": "ok", "autostart": s.autostart()})
}

// hosts returns the hosts with the number of tunnels of every type.
func (s *Server) hosts() []hostView {
	cfg := s.mgr.Config()
	out := make([]hostView, 0, len(cfg.Hosts))
	for _, h := range cfg.Hosts {
		out = append(out, hostView{
			ID:              h.ID,
			Name:            h.Name,
			Host:            h.Host,
			Port:            h.Port,
			User:            h.Auth.User,
			AuthType:        h.Auth.Type,
			HasPassword:     h.Auth.Type == config.AuthPassword && h.Auth.Password != "",
			HasKeyText:      h.Auth.Type == config.AuthKey && h.Auth.KeyText != "",
			HasPassphrase:   h.Auth.Type == config.AuthKey && h.Auth.Passphrase != "",
			KeyPath:         h.Auth.KeyPath,
			AutoReconnect:   h.AutoReconnect,
			InsecureHostKey: h.InsecureHostKey,
			Counts:          cfg.ForwardCounts(h.ID),
		})
	}
	return out
}

// tunnels returns the tunnels with the name of their host.
func (s *Server) tunnels() []tunnelView {
	cfg := s.mgr.Config()
	out := make([]tunnelView, 0, len(cfg.Tunnels))
	for _, t := range cfg.Tunnels {
		v := tunnelView{Tunnel: t, Mapping: t.Mapping()}
		if h, ok := cfg.HostByID(t.HostID); ok {
			v.HostName = h.DisplayName()
		}
		out = append(out, v)
	}
	return out
}

// saveHost creates or updates a host.
func (s *Server) saveHost(w http.ResponseWriter, r *http.Request) {
	var in hostInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	cfg := s.mgr.Config()
	old, _ := cfg.HostByID(in.ID)
	in.Auth.Type = authType(in.Auth, old)
	h := config.Host{
		ID:              in.ID,
		Name:            strings.TrimSpace(in.Name),
		Host:            strings.TrimSpace(in.Host),
		Port:            in.Port,
		Auth:            resolveAuth(in.Auth, old),
		AutoReconnect:   in.AutoReconnect,
		InsecureHostKey: in.InsecureHostKey,
	}
	if h.Port == 0 {
		h.Port = 22
	}
	if h.ID == "" && cfg.HostExists(h) {
		s.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, i18n.E("err.host_exists", h.UserAddress()))
		return
	}
	id := cfg.UpsertHost(h)
	err := cfg.Save(s.cfgPath)
	s.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.mgr.Reload(cfg)
	s.writeJSON(w, map[string]string{"result": "ok", "id": id})
}

// verifyHost checks that the authentication of a host works.
func (s *Server) verifyHost(w http.ResponseWriter, r *http.Request) {
	var in hostInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	cfg := s.mgr.Config()
	old, _ := cfg.HostByID(in.ID)
	h := config.Host{
		ID:              in.ID,
		Name:            strings.TrimSpace(in.Name),
		Host:            strings.TrimSpace(in.Host),
		Port:            in.Port,
		AutoReconnect:   in.AutoReconnect,
		InsecureHostKey: in.InsecureHostKey,
	}
	if old != nil {
		// A request that carries no address tests the stored host.
		if h.Host == "" {
			h.Host = old.Host
			if in.Port == 0 {
				h.Port = old.Port
			}
		}
	}
	if h.Port == 0 {
		h.Port = 22
	}
	in.Auth.Type = authType(in.Auth, old)
	h.Auth = resolveAuth(in.Auth, old)
	s.mu.Unlock()

	if errs := config.ValidateHost(&h); len(errs) > 0 {
		s.writeError(w, http.StatusBadRequest, errs[0])
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	client, err := sshclient.Dial(ctx, sshclient.Options{
		Host:     h,
		Settings: s.mgr.Config().Settings,
	})
	if err != nil {
		s.writeError(w, http.StatusBadRequest, i18n.E("err.auth_failed", err))
		return
	}
	client.Close()
	s.writeJSON(w, map[string]string{"result": "ok", "detail": i18n.T("msg.connected_to", h.Address())})
}

// deleteHost removes a host and every tunnel built on it.
func (s *Server) deleteHost(w http.ResponseWriter, id string) {
	s.mu.Lock()
	cfg := s.mgr.Config()
	stopped := make([]string, 0)
	for _, t := range cfg.TunnelsOfHost(id) {
		stopped = append(stopped, t.ID)
	}
	removed := cfg.RemoveHost(id)
	var err error
	if removed {
		err = cfg.Save(s.cfgPath)
	}
	s.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if removed {
		for _, tunnelID := range stopped {
			s.mgr.Stop(tunnelID)
		}
		s.mgr.Reload(cfg)
	}
	s.writeJSON(w, map[string]string{"result": "ok"})
}

// saveTunnel creates or updates a tunnel.
func (s *Server) saveTunnel(w http.ResponseWriter, r *http.Request) {
	var t config.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if t.Type == "" {
		t.Type = config.ForwardLocal
	}
	// A target address without a host points at the loopback address.
	config.NormalizeTunnel(&t)
	if errs := config.ValidateTunnel(&t); len(errs) > 0 {
		s.writeError(w, http.StatusBadRequest, errs[0])
		return
	}
	s.mu.Lock()
	cfg := s.mgr.Config()
	if _, ok := cfg.HostByID(t.HostID); !ok {
		s.mu.Unlock()
		s.writeError(w, http.StatusBadRequest, i18n.E("err.host_missing"))
		return
	}
	id := cfg.UpsertTunnel(t)
	err := cfg.Save(s.cfgPath)
	s.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.mgr.Reload(cfg)
	s.writeJSON(w, map[string]string{"result": "ok", "id": id})
}

// deleteTunnel removes a tunnel.
func (s *Server) deleteTunnel(w http.ResponseWriter, id string) {
	s.mu.Lock()
	cfg := s.mgr.Config()
	removed := cfg.RemoveTunnel(id)
	var err error
	if removed {
		err = cfg.Save(s.cfgPath)
	}
	s.mu.Unlock()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	if removed {
		s.mgr.Stop(id)
		s.mgr.Reload(cfg)
	}
	s.writeJSON(w, map[string]string{"result": "ok"})
}

func (s *Server) control(w http.ResponseWriter, id string, start bool) {
	if start {
		if err := s.mgr.Start(id); err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		s.mgr.Stop(id)
	}
	s.writeJSON(w, map[string]string{"result": "ok"})
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// writeForbidden answers a request that the program refuses. The code tells
// the interface what the reason is, so that it does not blame the token when
// the origin is the problem.
func (s *Server) writeForbidden(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]string{
		"error": i18n.T("err." + code),
		"code":  code,
	})
}
