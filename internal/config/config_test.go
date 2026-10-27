package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	host := NewHost()
	host.Name = "web"
	host.Host = "10.0.0.1"
	host.Port = 22
	host.Auth = Auth{Type: AuthKey, User: "root", KeyPath: "/tmp/id_ed25519"}
	hostID := cfg.UpsertHost(host)
	cfg.UpsertTunnel(Tunnel{
		HostID: hostID,
		Type:   ForwardLocal,
		Listen: "127.0.0.1:8080",
		Target: "127.0.0.1:80",
	})
	cfg.UpsertTunnel(Tunnel{
		HostID: hostID,
		Type:   ForwardSocks5,
		Listen: "127.0.0.1:1080",
	})
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Hosts) != 1 || len(loaded.Tunnels) != 2 {
		t.Fatalf("unexpected content: %d hosts, %d tunnels", len(loaded.Hosts), len(loaded.Tunnels))
	}
	h, ok := loaded.HostByID(hostID)
	if !ok {
		t.Fatal("host not found")
	}
	if h.Host != "10.0.0.1" || h.Port != 22 {
		t.Fatalf("unexpected host/port %s:%d", h.Host, h.Port)
	}
	if h.KeepaliveSeconds != 30 {
		t.Fatalf("keepalive default not applied: %d", h.KeepaliveSeconds)
	}
	tunnels := loaded.TunnelsOfHost(hostID)
	if len(tunnels) != 2 {
		t.Fatalf("unexpected tunnels %+v", tunnels)
	}
	if tunnels[0].Type != ForwardLocal || tunnels[0].Target != "127.0.0.1:80" {
		t.Fatalf("unexpected local tunnel %+v", tunnels[0])
	}
	if counts := loaded.ForwardCounts(hostID); counts[ForwardLocal] != 1 || counts[ForwardSocks5] != 1 {
		t.Fatalf("unexpected counts %+v", counts)
	}
}

func TestHostExists(t *testing.T) {
	cfg := Default()
	host := NewHost()
	host.Name = "web"
	host.Host = "Example.COM"
	host.Port = 22
	host.Auth.User = "root"
	cfg.UpsertHost(host)

	same := NewHost()
	same.Host = " example.com "
	same.Port = 0
	same.Auth.User = "root"
	if !cfg.HostExists(same) {
		t.Error("the same host with a different case and the default port was not found")
	}

	otherPort := same
	otherPort.Port = 2222
	if cfg.HostExists(otherPort) {
		t.Error("a host with a different port was treated as a duplicate")
	}

	otherUser := same
	otherUser.Auth.User = "deploy"
	if cfg.HostExists(otherUser) {
		t.Error("a host with a different user was treated as a duplicate")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Hosts) != 0 || len(cfg.Tunnels) != 0 {
		t.Fatalf("expected an empty config, got %+v", cfg)
	}
	if cfg.Settings.KeepaliveSeconds != 30 {
		t.Fatalf("unexpected default settings %+v", cfg.Settings)
	}
	if cfg.Settings.ThemeOrAuto() != ThemeAuto {
		t.Fatalf("unexpected default theme %q", cfg.Settings.Theme)
	}
}

func TestNormalizeTheme(t *testing.T) {
	cases := map[string]string{
		"auto":  ThemeAuto,
		" Dark": ThemeDark,
		"LIGHT": ThemeLight,
		"":      "",
		"sepia": "",
	}
	for in, want := range cases {
		if got := NormalizeTheme(in); got != want {
			t.Errorf("NormalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
	settings := Settings{Theme: "sepia"}
	if got := settings.ThemeOrAuto(); got != ThemeAuto {
		t.Errorf("ThemeOrAuto() = %q, want %q", got, ThemeAuto)
	}
}

func TestValidateHost(t *testing.T) {
	h := Host{Host: "", Port: 22, Auth: Auth{Type: AuthPassword, User: "u"}}
	if errs := ValidateHost(&h); len(errs) == 0 {
		t.Fatal("expected validation errors")
	}

	h = Host{Host: "h", Port: 22, Auth: Auth{Type: AuthPassword, User: "u", Password: "x"}}
	if errs := ValidateHost(&h); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestValidateTunnel(t *testing.T) {
	tun := Tunnel{HostID: "h1", Type: ForwardLocal, Listen: "127.0.0.1:8080", Target: "not-an-address"}
	if errs := ValidateTunnel(&tun); len(errs) == 0 {
		t.Fatal("expected an error for the invalid target")
	}

	tun = Tunnel{HostID: "h1", Type: ForwardLocal, Listen: "127.0.0.1:8080", Target: "127.0.0.1:80"}
	if errs := ValidateTunnel(&tun); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}

	// A SOCKS5 proxy without a target is dynamic and therefore valid.
	tun = Tunnel{HostID: "h1", Type: ForwardSocks5, Listen: "127.0.0.1:1080"}
	if errs := ValidateTunnel(&tun); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestUpsertAndRemove(t *testing.T) {
	cfg := Default()
	h := NewHost()
	h.ID = "h1"
	h.Host = "h"
	cfg.UpsertHost(h)
	if len(cfg.Hosts) != 1 {
		t.Fatalf("host not added: %+v", cfg.Hosts)
	}
	h.Host = "other"
	cfg.UpsertHost(h)
	if len(cfg.Hosts) != 1 {
		t.Fatalf("upsert created a duplicate: %+v", cfg.Hosts)
	}
	if got, _ := cfg.HostByID("h1"); got.Host != "other" {
		t.Fatalf("upsert did not replace the host: %+v", got)
	}

	cfg.UpsertTunnel(Tunnel{ID: "t1", HostID: "h1", Type: ForwardLocal, Listen: "127.0.0.1:1", Target: "127.0.0.1:2"})
	if !cfg.RemoveHost("h1") {
		t.Fatal("remove failed")
	}
	if len(cfg.Tunnels) != 0 {
		t.Fatalf("removing a host must remove its tunnels: %+v", cfg.Tunnels)
	}
	if cfg.RemoveHost("missing") {
		t.Fatal("remove of a missing host should report false")
	}
}

func TestKeyFileMustExist(t *testing.T) {
	h := Host{Host: "h", Port: 22, Auth: Auth{Type: AuthKey, User: "u", KeyPath: filepath.Join(t.TempDir(), "missing")}}
	if errs := ValidateHost(&h); len(errs) == 0 {
		t.Fatal("expected an error for the missing private key")
	}
	_ = os.Remove(h.Auth.KeyPath)
}

func TestNewHostUsesPasswordAuth(t *testing.T) {
	if got := NewHost().Auth.Type; got != AuthPassword {
		t.Fatalf("new host auth = %q, want %q", got, AuthPassword)
	}

	cfg := Default()
	cfg.Hosts = append(cfg.Hosts, Host{Host: "h", Port: 22, Auth: Auth{User: "u"}})
	cfg.ApplyDefaults()
	if got := cfg.Hosts[0].Auth.Type; got != AuthPassword {
		t.Fatalf("default host auth = %q, want %q", got, AuthPassword)
	}
}

func TestKeyTextSkipsThePathCheck(t *testing.T) {
	h := Host{Host: "h", Port: 22, Auth: Auth{
		Type:    AuthKey,
		User:    "u",
		KeyPath: filepath.Join(t.TempDir(), "missing"),
		KeyText: "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----\n",
	}}
	if errs := ValidateHost(&h); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
}

func TestKeyNeedsPathOrText(t *testing.T) {
	h := Host{Host: "h", Port: 22, Auth: Auth{Type: AuthKey, User: "u"}}
	if errs := ValidateHost(&h); len(errs) == 0 {
		t.Fatal("expected an error for a host without key path or key text")
	}
}

func TestDefaultPath(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("default dir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	if want := filepath.Join(home, ".config", "ssh-tunnel"); dir != want {
		t.Fatalf("unexpected state dir %q, want %q", dir, want)
	}

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	if want := filepath.Join(dir, "config.json"); path != want {
		t.Fatalf("unexpected config path %q, want %q", path, want)
	}
}

// TestLoadMigratesLegacyConfig checks that a configuration file in the old
// state directory is copied to the new default location.
func TestLoadMigratesLegacyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	legacyPath := filepath.Join(home, ".ssh-tunnel", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	legacy := []byte(`{"hosts":[{"id":"h1","host":"legacy.example","port":22,"auth":{"type":"password","user":"root","password":"secret"}}]}`)
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Host != "legacy.example" {
		t.Fatalf("legacy config was not migrated: %+v", cfg.Hosts)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("migrated config is missing: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy config should be kept: %v", err)
	}
}

// TestMigrateLegacyProfiles checks that a configuration file written by the
// previous version is converted into hosts and tunnels.
func TestMigrateLegacyProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := map[string]any{
		"profiles": []map[string]any{{
			"name":   "old",
			"host":   "10.0.0.2",
			"port":   2222,
			"auth":   map[string]any{"type": "key", "user": "root", "keyPath": "/tmp/id"},
			"local":  []map[string]string{{"listen": "127.0.0.1:8080", "remote": "127.0.0.1:80"}},
			"remote": []map[string]string{{"remoteListen": "0.0.0.0:9000", "remote": "127.0.0.1:3000"}},
			"socks5": map[string]string{"listen": "127.0.0.1:1080"},
		}},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Hosts) != 1 || len(cfg.Tunnels) != 3 {
		t.Fatalf("unexpected migration result: %d hosts, %d tunnels", len(cfg.Hosts), len(cfg.Tunnels))
	}
	h := cfg.Hosts[0]
	if h.Name != "old" || h.Host != "10.0.0.2" || h.Port != 2222 {
		t.Fatalf("unexpected host %+v", h)
	}
	counts := cfg.ForwardCounts(h.ID)
	if counts[ForwardLocal] != 1 || counts[ForwardRemote] != 1 || counts[ForwardSocks5] != 1 {
		t.Fatalf("unexpected counts %+v", counts)
	}
}
