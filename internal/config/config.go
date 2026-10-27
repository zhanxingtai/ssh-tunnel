// Package config defines the persisted configuration of ssh-tunnel and the
// rules used to validate it.
//
// The configuration has two levels: hosts hold the connection details (address
// and authentication) and tunnels hold the mappings built on top of a host
// (local forward, remote forward or SOCKS5 proxy).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"ssh-tunnel/internal/i18n"
)

// Supported authentication methods.
const (
	AuthPassword = "password"
	AuthKey      = "key"
	AuthAgent    = "agent"
)

// Interface themes.
const (
	// ThemeAuto follows the color scheme of the system.
	ThemeAuto = "auto"
	// ThemeDark always uses the dark color scheme.
	ThemeDark = "dark"
	// ThemeLight always uses the light color scheme.
	ThemeLight = "light"
)

// Tunnel types.
const (
	// ForwardLocal listens locally and forwards every connection to a
	// destination reached through the SSH server.
	ForwardLocal = "local"
	// ForwardRemote asks the SSH server to listen and forwards every accepted
	// connection to a local destination.
	ForwardRemote = "remote"
	// ForwardSocks5 is a local SOCKS5 proxy. Without a target it is a dynamic
	// proxy: every destination is reached through the SSH server.
	ForwardSocks5 = "socks5"
)

// Auth describes how the client authenticates against the SSH server.
type Auth struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	// KeyText holds the text of a private key that was pasted or read from a
	// file in the interface. It takes precedence over KeyPath.
	KeyText string `json:"keyText,omitempty"`
	// Passphrase decrypts an encrypted private key. Empty means the key is
	// not encrypted.
	Passphrase string `json:"passphrase,omitempty"`
}

// Host is a reachable SSH server together with its credentials.
type Host struct {
	// ID identifies the host inside the configuration file.
	ID string `json:"id"`
	// Name is the display name shown in the interface. It falls back to the
	// address of the host when empty.
	Name string `json:"name,omitempty"`
	// Host is the address of the SSH server.
	Host string `json:"host"`
	// Port is the TCP port of the SSH server.
	Port int `json:"port"`

	Auth Auth `json:"auth"`

	// KeepaliveSeconds is the interval of SSH keepalive probes. Zero falls
	// back to the global setting.
	KeepaliveSeconds int `json:"keepaliveSeconds,omitempty"`
	// ConnectTimeoutSeconds bounds the SSH handshake. Zero falls back to the
	// global setting.
	ConnectTimeoutSeconds int `json:"connectTimeoutSeconds,omitempty"`
	// AutoReconnect re-establishes the tunnel after a network failure.
	AutoReconnect bool `json:"autoReconnect"`
	// InsecureHostKey skips the known_hosts check. Never enable it for
	// production hosts.
	InsecureHostKey bool `json:"insecureHostKey"`
}

// DisplayName returns the name shown in the interface.
func (h Host) DisplayName() string {
	if strings.TrimSpace(h.Name) != "" {
		return strings.TrimSpace(h.Name)
	}
	return h.Address()
}

// Address returns the "host:port" pair of the SSH server.
func (h Host) Address() string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(h.Host, strconv.Itoa(port))
}

// UserAddress returns the user and the address of the host.
func (h Host) UserAddress() string {
	user := strings.TrimSpace(h.Auth.User)
	if user == "" {
		return h.Address()
	}
	return user + "@" + h.Address()
}

// Tunnel is a single mapping built on top of a host.
type Tunnel struct {
	// ID identifies the tunnel inside the configuration file.
	ID string `json:"id"`
	// Name is the display name shown in the interface. It falls back to the
	// type and the listening address when empty.
	Name string `json:"name,omitempty"`
	// HostID is the host the tunnel is built on.
	HostID string `json:"hostId"`
	// Type is one of ForwardLocal, ForwardRemote and ForwardSocks5.
	Type string `json:"type"`
	// Listen is the local address for a local forward or a SOCKS5 proxy and
	// the remote address for a remote forward.
	Listen string `json:"listen,omitempty"`
	// Target is the destination of the mapping. It is resolved by the SSH
	// server for a local forward and locally for a remote forward. It is
	// optional for a SOCKS5 proxy.
	Target string `json:"target,omitempty"`
	// AutoStart starts the tunnel as soon as the application runs.
	AutoStart bool `json:"autoStart"`
}

// DisplayName returns the name shown in the interface.
func (t Tunnel) DisplayName() string {
	if strings.TrimSpace(t.Name) != "" {
		return strings.TrimSpace(t.Name)
	}
	if t.Listen != "" {
		return fmt.Sprintf("%s %s", TypeLabel(t.Type), t.Listen)
	}
	return TypeLabel(t.Type)
}

// TypeLabel returns the human readable name of a tunnel type.
func TypeLabel(t string) string {
	switch t {
	case ForwardLocal:
		return i18n.T("type.local")
	case ForwardRemote:
		return i18n.T("type.remote")
	case ForwardSocks5:
		return i18n.T("type.socks5")
	default:
		return t
	}
}

// Settings holds the defaults shared by every host.
type Settings struct {
	KeepaliveSeconds         int `json:"keepaliveSeconds"`
	ConnectTimeoutSeconds    int `json:"connectTimeoutSeconds"`
	ReconnectDelaySeconds    int `json:"reconnectDelaySeconds"`
	MaxReconnectDelaySeconds int `json:"maxReconnectDelaySeconds"`
	// AutoReconnectDefault is used for hosts that do not override it.
	AutoReconnectDefault bool `json:"autoReconnectDefault"`
	// Language is the locale of the interface and of the messages, "zh-CN" or
	// "en-US". An empty value means that the locale of the system is used.
	Language string `json:"language,omitempty"`
	// Theme is the color scheme of the interface: "auto", "dark" or "light".
	// An empty value means that the color scheme of the system is used.
	Theme string `json:"theme,omitempty"`
}

// NormalizeTheme maps the spellings of a theme onto the supported ones. It
// returns an empty string when the theme is not supported.
func NormalizeTheme(theme string) string {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case ThemeAuto:
		return ThemeAuto
	case ThemeDark:
		return ThemeDark
	case ThemeLight:
		return ThemeLight
	default:
		return ""
	}
}

// ThemeOrAuto returns the theme of the configuration, defaulting to the one
// of the system.
func (s Settings) ThemeOrAuto() string {
	if theme := NormalizeTheme(s.Theme); theme != "" {
		return theme
	}
	return ThemeAuto
}

// Language returns the locale of the configuration. It falls back to the
// locale of the system when the configuration does not choose one.
func (s Settings) LanguageOrDetect() string {
	if l := i18n.Normalize(s.Language); l != "" {
		return l
	}
	return i18n.Detect()
}

// Config is the whole configuration file.
type Config struct {
	Settings Settings `json:"settings"`
	// Hosts holds the SSH servers, in the order they are shown.
	Hosts []Host `json:"hosts"`
	// Tunnels holds the mappings, in the order they are shown.
	Tunnels []Tunnel `json:"tunnels"`
}

// DefaultSettings returns the built-in defaults.
func DefaultSettings() Settings {
	return Settings{
		KeepaliveSeconds:         30,
		ConnectTimeoutSeconds:    10,
		ReconnectDelaySeconds:    3,
		MaxReconnectDelaySeconds: 60,
		AutoReconnectDefault:     true,
		Theme:                    ThemeAuto,
	}
}

// NewHost returns a host pre-filled with the default values.
func NewHost() Host {
	return Host{
		Port:                  22,
		Auth:                  Auth{Type: AuthPassword, User: "root"},
		KeepaliveSeconds:      DefaultSettings().KeepaliveSeconds,
		ConnectTimeoutSeconds: DefaultSettings().ConnectTimeoutSeconds,
		AutoReconnect:         true,
	}
}

// NewTunnel returns a tunnel pre-filled with the default values.
func NewTunnel(hostID string) Tunnel {
	return Tunnel{
		HostID: hostID,
		Type:   ForwardLocal,
	}
}

// Default returns an empty configuration with default settings.
func Default() *Config {
	return &Config{Settings: DefaultSettings()}
}

// fileConfig is the on-disk layout. The legacy profiles are read so that an
// old configuration file can be migrated to the host/tunnel model.
type fileConfig struct {
	Settings Settings        `json:"settings"`
	Hosts    []Host          `json:"hosts"`
	Tunnels  []Tunnel        `json:"tunnels"`
	Profiles []legacyProfile `json:"profiles"`
}

// legacyProfile is the profile layout used before hosts and tunnels were
// separated.
type legacyProfile struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Auth  Auth   `json:"auth"`
	Local []struct {
		Listen string `json:"listen"`
		Remote string `json:"remote"`
	} `json:"local"`
	Remote []struct {
		RemoteListen string `json:"remoteListen"`
		Remote       string `json:"remote"`
	} `json:"remote"`
	Socks5 *struct {
		Listen string `json:"listen"`
		Target string `json:"target"`
	} `json:"socks5"`
	KeepaliveSeconds      int  `json:"keepaliveSeconds"`
	ConnectTimeoutSeconds int  `json:"connectTimeoutSeconds"`
	AutoReconnect         bool `json:"autoReconnect"`
	InsecureHostKey       bool `json:"insecureHostKey"`
}

// Load reads the configuration file. A missing file is not an error: the
// default configuration is returned instead.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := migrateLegacyConfig(path); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(path)
		if os.IsNotExist(err) {
			return Default(), nil
		}
	}
	if err != nil {
		return nil, i18n.E("err.read_config", path, err)
	}
	var file fileConfig
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, i18n.E("err.parse_config", path, err)
	}
	cfg := &Config{Settings: file.Settings, Hosts: file.Hosts, Tunnels: file.Tunnels}
	if len(cfg.Hosts) == 0 && len(file.Profiles) > 0 {
		migrateLegacy(cfg, file.Profiles)
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

// Save writes the configuration file atomically.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return i18n.E("err.create_config_dir", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return i18n.E("err.encode_config", err)
	}
	data = append(data, '\n')
	return writeConfigFile(path, data)
}

// writeConfigFile writes data to path, replacing the file atomically.
func writeConfigFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return i18n.E("err.write_config", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return i18n.E("err.replace_config", err)
	}
	return nil
}

// migrateLegacyConfig copies the configuration of versions that kept the
// user state in ~/.ssh-tunnel to the new default location. The old file is
// left in place as a backup. Only the default path is migrated; an explicit
// --config file is used as given.
func migrateLegacyConfig(path string) error {
	defaultPath, err := DefaultPath()
	if err != nil || !samePath(path, defaultPath) {
		return nil
	}
	legacyPath, err := legacyConfigPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return i18n.E("err.read_config", legacyPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return i18n.E("err.create_config_dir", err)
	}
	return writeConfigFile(path, data)
}

// legacyConfigPath returns the configuration file used before the user state
// moved to the .config directory.
func legacyConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.E("err.locate_home", err)
	}
	return filepath.Join(home, legacyDirName, "config.json"), nil
}

// samePath reports whether two paths refer to the same file, taking the
// case-insensitive Windows file system into account.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	return runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

// migrateLegacy converts the old profiles into hosts and tunnels.
func migrateLegacy(cfg *Config, profiles []legacyProfile) {
	for _, p := range profiles {
		h := Host{
			ID:                    newID("h"),
			Name:                  p.Name,
			Host:                  p.Host,
			Port:                  p.Port,
			Auth:                  p.Auth,
			KeepaliveSeconds:      p.KeepaliveSeconds,
			ConnectTimeoutSeconds: p.ConnectTimeoutSeconds,
			AutoReconnect:         p.AutoReconnect,
			InsecureHostKey:       p.InsecureHostKey,
		}
		cfg.Hosts = append(cfg.Hosts, h)
		for _, f := range p.Local {
			cfg.Tunnels = append(cfg.Tunnels, Tunnel{
				ID:     newID("t"),
				HostID: h.ID,
				Type:   ForwardLocal,
				Listen: f.Listen,
				Target: f.Remote,
			})
		}
		for _, f := range p.Remote {
			cfg.Tunnels = append(cfg.Tunnels, Tunnel{
				ID:     newID("t"),
				HostID: h.ID,
				Type:   ForwardRemote,
				Listen: f.RemoteListen,
				Target: f.Remote,
			})
		}
		if p.Socks5 != nil {
			cfg.Tunnels = append(cfg.Tunnels, Tunnel{
				ID:     newID("t"),
				HostID: h.ID,
				Type:   ForwardSocks5,
				Listen: p.Socks5.Listen,
				Target: p.Socks5.Target,
			})
		}
	}
}

// ApplyDefaults fills in the zero values that have a meaningful default.
func (c *Config) ApplyDefaults() {
	def := DefaultSettings()
	if c.Settings.KeepaliveSeconds <= 0 {
		c.Settings.KeepaliveSeconds = def.KeepaliveSeconds
	}
	if c.Settings.ConnectTimeoutSeconds <= 0 {
		c.Settings.ConnectTimeoutSeconds = def.ConnectTimeoutSeconds
	}
	if c.Settings.ReconnectDelaySeconds <= 0 {
		c.Settings.ReconnectDelaySeconds = def.ReconnectDelaySeconds
	}
	if c.Settings.MaxReconnectDelaySeconds <= 0 {
		c.Settings.MaxReconnectDelaySeconds = def.MaxReconnectDelaySeconds
	}
	if theme := NormalizeTheme(c.Settings.Theme); theme != "" {
		c.Settings.Theme = theme
	} else {
		c.Settings.Theme = def.Theme
	}
	for i := range c.Hosts {
		h := &c.Hosts[i]
		if h.ID == "" {
			h.ID = newID("h")
		}
		if h.Port == 0 {
			h.Port = 22
		}
		if h.Auth.Type == "" {
			h.Auth.Type = AuthPassword
		}
		if h.KeepaliveSeconds <= 0 {
			h.KeepaliveSeconds = c.Settings.KeepaliveSeconds
		}
		if h.ConnectTimeoutSeconds <= 0 {
			h.ConnectTimeoutSeconds = c.Settings.ConnectTimeoutSeconds
		}
	}
	for i := range c.Tunnels {
		t := &c.Tunnels[i]
		if t.ID == "" {
			t.ID = newID("t")
		}
		if t.Type == "" {
			t.Type = ForwardLocal
		}
	}
	// Make sure every identifier is unique.
	seenHost := map[string]bool{}
	for i := range c.Hosts {
		for seenHost[c.Hosts[i].ID] {
			c.Hosts[i].ID = newID("h")
		}
		seenHost[c.Hosts[i].ID] = true
	}
	seenTunnel := map[string]bool{}
	for i := range c.Tunnels {
		for seenTunnel[c.Tunnels[i].ID] {
			c.Tunnels[i].ID = newID("t")
		}
		seenTunnel[c.Tunnels[i].ID] = true
	}
}

// HostByID returns the host with the given identifier.
func (c *Config) HostByID(id string) (*Host, bool) {
	for i := range c.Hosts {
		if c.Hosts[i].ID == id {
			return &c.Hosts[i], true
		}
	}
	return nil, false
}

// HostExists reports whether a host with the same user, host, and port exists.
// The identifier is ignored.
func (c *Config) HostExists(h Host) bool {
	host := strings.TrimSpace(h.Host)
	user := strings.TrimSpace(h.Auth.User)
	port := h.Port
	if port == 0 {
		port = 22
	}
	for i := range c.Hosts {
		other := &c.Hosts[i]
		otherPort := other.Port
		if otherPort == 0 {
			otherPort = 22
		}
		if otherPort == port &&
			strings.EqualFold(strings.TrimSpace(other.Host), host) &&
			strings.TrimSpace(other.Auth.User) == user {
			return true
		}
	}
	return false
}

// HostByRef returns the host matching an identifier or a display name.
func (c *Config) HostByRef(ref string) (*Host, bool) {
	if h, ok := c.HostByID(ref); ok {
		return h, true
	}
	for i := range c.Hosts {
		if strings.EqualFold(c.Hosts[i].DisplayName(), ref) || strings.EqualFold(c.Hosts[i].Name, ref) {
			return &c.Hosts[i], true
		}
	}
	return nil, false
}

// TunnelByID returns the tunnel with the given identifier.
func (c *Config) TunnelByID(id string) (*Tunnel, bool) {
	for i := range c.Tunnels {
		if c.Tunnels[i].ID == id {
			return &c.Tunnels[i], true
		}
	}
	return nil, false
}

// TunnelByRef returns the tunnel matching an identifier or a display name.
func (c *Config) TunnelByRef(ref string) (*Tunnel, bool) {
	if t, ok := c.TunnelByID(ref); ok {
		return t, true
	}
	for i := range c.Tunnels {
		if strings.EqualFold(c.Tunnels[i].DisplayName(), ref) || strings.EqualFold(c.Tunnels[i].Name, ref) {
			return &c.Tunnels[i], true
		}
	}
	return nil, false
}

// TunnelsOfHost returns the tunnels built on the given host.
func (c *Config) TunnelsOfHost(hostID string) []Tunnel {
	out := make([]Tunnel, 0)
	for _, t := range c.Tunnels {
		if t.HostID == hostID {
			out = append(out, t)
		}
	}
	return out
}

// ForwardCounts returns how many tunnels of every type the host owns.
func (c *Config) ForwardCounts(hostID string) map[string]int {
	counts := map[string]int{}
	for _, t := range c.Tunnels {
		if t.HostID == hostID {
			counts[t.Type]++
		}
	}
	return counts
}

// UpsertHost inserts or replaces a host and returns its identifier.
func (c *Config) UpsertHost(h Host) string {
	if h.ID == "" {
		h.ID = newID("h")
	}
	for i := range c.Hosts {
		if c.Hosts[i].ID == h.ID {
			c.Hosts[i] = h
			return h.ID
		}
	}
	c.Hosts = append(c.Hosts, h)
	return h.ID
}

// RemoveHost deletes a host and every tunnel built on it. It reports whether
// the host existed.
func (c *Config) RemoveHost(id string) bool {
	for i := range c.Hosts {
		if c.Hosts[i].ID != id {
			continue
		}
		c.Hosts = append(c.Hosts[:i], c.Hosts[i+1:]...)
		tunnels := c.Tunnels[:0]
		for _, t := range c.Tunnels {
			if t.HostID != id {
				tunnels = append(tunnels, t)
			}
		}
		c.Tunnels = tunnels
		return true
	}
	return false
}

// UpsertTunnel inserts or replaces a tunnel and returns its identifier.
func (c *Config) UpsertTunnel(t Tunnel) string {
	if t.ID == "" {
		t.ID = newID("t")
	}
	for i := range c.Tunnels {
		if c.Tunnels[i].ID == t.ID {
			c.Tunnels[i] = t
			return t.ID
		}
	}
	c.Tunnels = append(c.Tunnels, t)
	return t.ID
}

// RemoveTunnel deletes a tunnel and reports whether it existed.
func (c *Config) RemoveTunnel(id string) bool {
	for i := range c.Tunnels {
		if c.Tunnels[i].ID == id {
			c.Tunnels = append(c.Tunnels[:i], c.Tunnels[i+1:]...)
			return true
		}
	}
	return false
}

// ValidateHost checks a single host and returns every problem found.
func ValidateHost(h *Host) []error {
	var errs []error
	if strings.TrimSpace(h.Host) == "" {
		errs = append(errs, i18n.E("err.host_address_required"))
	}
	if h.Port < 1 || h.Port > 65535 {
		errs = append(errs, i18n.E("err.port_out_of_range", h.Port))
	}
	if strings.TrimSpace(h.Auth.User) == "" {
		errs = append(errs, i18n.E("err.ssh_user_required"))
	}
	switch h.Auth.Type {
	case AuthPassword:
		if h.Auth.Password == "" {
			errs = append(errs, i18n.E("err.password_required"))
		}
	case AuthKey:
		if strings.TrimSpace(h.Auth.KeyPath) == "" && strings.TrimSpace(h.Auth.KeyText) == "" {
			errs = append(errs, i18n.E("err.key_required"))
		} else if strings.TrimSpace(h.Auth.KeyText) == "" {
			if _, err := os.Stat(h.Auth.KeyPath); err != nil {
				errs = append(errs, i18n.E("err.key_not_readable", h.Auth.KeyPath, err))
			}
		}
	case AuthAgent:
	default:
		errs = append(errs, i18n.E("err.unknown_auth_type", h.Auth.Type))
	}
	return errs
}

// ValidateTunnel checks a single tunnel and returns every problem found.
func ValidateTunnel(t *Tunnel) []error {
	var errs []error
	if strings.TrimSpace(t.HostID) == "" {
		errs = append(errs, i18n.E("err.tunnel_needs_host"))
	}
	switch t.Type {
	case ForwardLocal, ForwardRemote, ForwardSocks5:
	default:
		errs = append(errs, i18n.E("err.unknown_tunnel_type", t.Type))
	}
	if err := checkHostPort(i18n.T("field.listen_address"), t.Listen); err != nil {
		errs = append(errs, err)
	}
	if t.Type == ForwardSocks5 {
		// A SOCKS5 proxy is dynamic when no target is given.
		if t.Target != "" {
			if err := checkHostPort(i18n.T("field.target_address"), t.Target); err != nil {
				errs = append(errs, err)
			}
		}
		return errs
	}
	if err := checkHostPort(i18n.T("field.target_address"), t.Target); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// Validate checks the whole configuration.
func (c *Config) Validate() []error {
	var errs []error
	for i := range c.Hosts {
		for _, err := range ValidateHost(&c.Hosts[i]) {
			errs = append(errs, i18n.E("err.host_prefix", c.Hosts[i].DisplayName(), err))
		}
	}
	for i := range c.Tunnels {
		t := &c.Tunnels[i]
		for _, err := range ValidateTunnel(t) {
			errs = append(errs, i18n.E("err.tunnel_prefix", t.DisplayName(), err))
		}
		if _, ok := c.HostByID(t.HostID); !ok {
			errs = append(errs, i18n.E("err.unknown_host_ref", t.DisplayName(), t.HostID))
		}
	}
	return errs
}

func checkHostPort(label, addr string) error {
	if addr == "" {
		return i18n.E("err.address_required", label)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return i18n.E("err.address_invalid", label, addr, err)
	}
	if host == "" {
		return i18n.E("err.address_missing_host", label, addr)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return i18n.E("err.address_invalid_port", label, addr)
	}
	return nil
}

const (
	// DirName is the name of the directory holding the user state inside the
	// configuration directory under the home directory.
	DirName = "ssh-tunnel"
	// configDirName is the XDG-style configuration directory. It is used on
	// every platform so that the location is the same everywhere.
	configDirName = ".config"
	// legacyDirName is the state directory of the versions before the state
	// moved to the .config directory.
	legacyDirName = ".ssh-tunnel"
)

// DefaultDir returns the directory holding the user state: .config/ssh-tunnel
// in the home directory of the current user.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.E("err.locate_home", err)
	}
	return filepath.Join(home, configDirName, DirName), nil
}

// DefaultPath returns the configuration file location for the current user.
func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// newID returns a random identifier with the given prefix.
func newID(prefix string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// The identifier only has to be unique inside the file.
		return fmt.Sprintf("%s%d", prefix, len(b))
	}
	return prefix + hex.EncodeToString(b[:])
}

// SortHosts sorts the hosts by display name.
func SortHosts(hosts []Host) {
	sort.SliceStable(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].DisplayName()) < strings.ToLower(hosts[j].DisplayName())
	})
}
