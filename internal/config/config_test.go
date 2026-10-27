package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ssh-tunnel/internal/secret"
)

// TestMain keeps the tests away from the keyring of the system: the
// credentials of the tests stay in the encrypted file of the test directory.
func TestMain(m *testing.M) {
	os.Setenv("SSH_TUNNEL_KEYRING", "0")
	os.Exit(m.Run())
}

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

// TestCredentialsStayOutOfTheFile checks that the credentials of a host are
// kept in the secret store and never reach the configuration file.
func TestCredentialsStayOutOfTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	host := NewHost()
	host.Host = "10.0.0.1"
	host.Auth.Password = "s3cret"
	id := cfg.UpsertHost(host)
	key := Host{Host: "10.0.0.2", Port: 22, Auth: Auth{
		Type:       AuthKey,
		User:       "root",
		KeyText:    "-----BEGIN OPENSSH PRIVATE KEY-----",
		Passphrase: "phrase",
	}}
	keyID := cfg.UpsertHost(key)
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration file: %v", err)
	}
	for _, secret := range []string{"s3cret", "BEGIN OPENSSH PRIVATE KEY", "phrase"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("the configuration file holds %q", secret)
		}
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	h, ok := loaded.HostByID(id)
	if !ok {
		t.Fatal("host not found")
	}
	if h.Auth.Password != "s3cret" {
		t.Fatalf("the password was not read back: %+v", h.Auth)
	}
	k, ok := loaded.HostByID(keyID)
	if !ok {
		t.Fatal("host not found")
	}
	if k.Auth.KeyText != key.Auth.KeyText || k.Auth.Passphrase != "phrase" {
		t.Fatalf("the credentials were not read back: %+v", k.Auth)
	}
}

// TestCredentialsOfAnotherWayAreKept checks that a credential that does not
// belong to the way of authentication of a host is moved to the store
// instead of being lost.
func TestCredentialsOfAnotherWayAreKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := []byte(`{"hosts":[{"id":"h1","host":"10.0.0.1","port":22,"auth":{"type":"password","user":"root","password":"s3cret","passphrase":"phrase"}}]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write the configuration file: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Hosts[0].Auth.Password != "s3cret" {
		t.Fatalf("the password was not read: %+v", cfg.Hosts[0].Auth)
	}
	if cfg.Hosts[0].Auth.Passphrase != "phrase" {
		t.Fatalf("the passphrase was lost: %+v", cfg.Hosts[0].Auth)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration file: %v", err)
	}
	if bytes.Contains(data, []byte("s3cret")) || bytes.Contains(data, []byte("phrase")) {
		t.Error("the configuration file still holds a credential")
	}

	// The credentials are in the store, so they are read back.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if loaded.Hosts[0].Auth.Password != "s3cret" || loaded.Hosts[0].Auth.Passphrase != "phrase" {
		t.Fatalf("the credentials were not read back: %+v", loaded.Hosts[0].Auth)
	}
}

// TestAnUnknownAuthTypeKeepsTheCredentials checks that a way of
// authentication that the program does not know does not cost the user the
// credentials of the host.
func TestAnUnknownAuthTypeKeepsTheCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := []byte(`{"hosts":[{"id":"h1","host":"10.0.0.1","port":22,"auth":{"type":"Password","user":"root","password":"s3cret"}}]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write the configuration file: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Hosts[0].Auth.Type != AuthPassword {
		t.Fatalf("the way of authentication was not normalized: %q", cfg.Hosts[0].Auth.Type)
	}
	if cfg.Hosts[0].Auth.Password != "s3cret" {
		t.Fatalf("the password was lost: %+v", cfg.Hosts[0].Auth)
	}
}

// TestLegacyCredentialsAreMovedToTheStore checks that the credentials of a
// configuration file written by an older version are moved to the secret
// store and removed from the file.
func TestLegacyCredentialsAreMovedToTheStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := []byte(`{"hosts":[{"id":"h1","host":"10.0.0.1","port":22,"auth":{"type":"password","user":"root","password":"s3cret"}}]}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write the configuration file: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Auth.Password != "s3cret" {
		t.Fatalf("the password was not read: %+v", cfg.Hosts)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration file: %v", err)
	}
	if bytes.Contains(data, []byte("s3cret")) {
		t.Error("the configuration file still holds the password")
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if loaded.Hosts[0].Auth.Password != "s3cret" {
		t.Fatalf("the password was not kept in the store: %+v", loaded.Hosts[0].Auth)
	}
}

// fakeStore is a store of credentials for the tests. Its reads can be made
// to fail, like a keyring that is locked.
type fakeStore struct {
	dir     string
	values  map[string]string
	failGet bool
}

func newFakeStore(dir string) *fakeStore {
	return &fakeStore{dir: dir, values: map[string]string{}}
}

func (s *fakeStore) Get(hostID string, f secret.Field) (string, bool, error) {
	if s.failGet {
		return "", false, errors.New("the store is not answering")
	}
	value, ok := s.values[hostID+":"+string(f)]
	return value, ok, nil
}

func (s *fakeStore) Put(hostID string, f secret.Field, value string) error {
	s.values[hostID+":"+string(f)] = value
	return nil
}

func (s *fakeStore) Delete(hostID string, f secret.Field) error {
	delete(s.values, hostID+":"+string(f))
	return nil
}

func (s *fakeStore) DeleteHost(hostID string) error {
	for _, f := range secret.Fields {
		s.Delete(hostID, f)
	}
	return nil
}

func (s *fakeStore) Dir() string { return s.dir }

// TestACredentialThatCouldNotBeReadIsNotDeleted makes sure that a credential
// the store could not read is not removed by a later save.
func TestACredentialThatCouldNotBeReadIsNotDeleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"hosts":[{"id":"h1","host":"10.0.0.1","port":22,"auth":{"type":"password","user":"root"}}]}`), 0o600); err != nil {
		t.Fatalf("write the configuration file: %v", err)
	}

	store := newFakeStore(dir)
	store.values["h1:password"] = "s3cret"
	store.failGet = true

	cfg, err := LoadWith(path, store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.CredentialError() == nil {
		t.Error("the error of the store was not reported")
	}
	if cfg.Hosts[0].Auth.Password != "" {
		t.Fatalf("the credential was read: %+v", cfg.Hosts[0].Auth)
	}

	// Saving the configuration must not remove the credential.
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := store.values["h1:password"]; !ok {
		t.Error("the credential was deleted although it could not be read")
	}
}

// TestTheCredentialsOfARemovedHostAreDeleted checks that the credentials of a
// host are removed with the host.
func TestTheCredentialsOfARemovedHostAreDeleted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := newFakeStore(dir)

	cfg := Default()
	cfg.SetStore(store)
	host := NewHost()
	host.Host = "10.0.0.1"
	host.Auth.Password = "s3cret"
	id := cfg.UpsertHost(host)
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := store.values[id+":password"]; !ok {
		t.Fatal("the password was not stored")
	}

	if !cfg.RemoveHost(id) {
		t.Fatal("the host was not removed")
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := store.values[id+":password"]; ok {
		t.Error("the password of the removed host is still stored")
	}
}
