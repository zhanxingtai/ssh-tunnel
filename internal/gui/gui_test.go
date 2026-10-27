package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/logging"
	"ssh-tunnel/internal/manager"
	"ssh-tunnel/internal/version"
)

// testServer starts a server on a temporary configuration file.
func testServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := config.Default()
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save the configuration: %v", err)
	}
	log, err := logging.New(filepath.Join(dir, "ssh-tunnel.log"))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	srv := New(manager.New(cfg, log), path)
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, path
}

// get fetches a path of the API.
func get(t *testing.T, ts *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// post sends a payload to a path of the API.
func post(t *testing.T, ts *httptest.Server, path string, payload any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode the payload: %v", err)
	}
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// loadSettings reads the settings of the configuration file.
func loadSettings(t *testing.T, path string) config.Settings {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	return cfg.Settings
}

func TestSettingsAreServed(t *testing.T) {
	ts, _ := testServer(t)
	code, body := get(t, ts, "/api/settings")
	if code != http.StatusOK {
		t.Fatalf("the settings are not served: %d", code)
	}
	settings, ok := body["settings"].(map[string]any)
	if !ok {
		t.Fatalf("the settings are missing: %v", body)
	}
	if settings["keepaliveSeconds"].(float64) != 30 {
		t.Errorf("unexpected keepalive %v", settings["keepaliveSeconds"])
	}
	if settings["theme"] != config.ThemeAuto {
		t.Errorf("unexpected theme %v", settings["theme"])
	}
	if _, ok := body["autostart"]; !ok {
		t.Errorf("the autostart is missing: %v", body)
	}
}

func TestSaveSettings(t *testing.T) {
	ts, path := testServer(t)
	code, _ := post(t, ts, "/api/settings", map[string]any{"keepaliveSeconds": 45})
	if code != http.StatusOK {
		t.Fatalf("the settings are not saved: %d", code)
	}
	settings := loadSettings(t, path)
	if settings.KeepaliveSeconds != 45 {
		t.Errorf("the keepalive is %d", settings.KeepaliveSeconds)
	}
	// The settings that were not sent keep their value.
	if settings.ConnectTimeoutSeconds != 10 {
		t.Errorf("the connection timeout is %d", settings.ConnectTimeoutSeconds)
	}
}

func TestSaveSettingsRefusesAnInvalidValue(t *testing.T) {
	ts, _ := testServer(t)
	code, body := post(t, ts, "/api/settings", map[string]any{"keepaliveSeconds": 0})
	if code != http.StatusBadRequest {
		t.Fatalf("an invalid value is accepted: %d", code)
	}
	if body["error"] == nil {
		t.Errorf("the error is missing: %v", body)
	}
}

func TestSaveSettingsRefusesAnUnknownLanguage(t *testing.T) {
	ts, _ := testServer(t)
	code, _ := post(t, ts, "/api/settings", map[string]any{"language": "fr"})
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown language is accepted: %d", code)
	}
}

func TestSaveSettingsRefusesAnUnknownTheme(t *testing.T) {
	ts, _ := testServer(t)
	code, _ := post(t, ts, "/api/settings", map[string]any{"theme": "sepia"})
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown theme is accepted: %d", code)
	}
}

func TestSaveTheThemeOnly(t *testing.T) {
	ts, path := testServer(t)
	code, body := post(t, ts, "/api/settings", map[string]any{"theme": "Light"})
	if code != http.StatusOK {
		t.Fatalf("the theme is not saved: %d %v", code, body)
	}
	if body["theme"] != config.ThemeLight {
		t.Errorf("unexpected theme %v", body["theme"])
	}
	if loadSettings(t, path).Theme != config.ThemeLight {
		t.Errorf("the theme was not written to the configuration")
	}
}

func TestSaveTheLanguageOnly(t *testing.T) {
	defer i18n.Set(i18n.Get())
	ts, path := testServer(t)
	code, body := post(t, ts, "/api/settings", map[string]any{"language": "en-US"})
	if code != http.StatusOK {
		t.Fatalf("the language is not saved: %d", code)
	}
	if body["language"] != "en-US" {
		t.Errorf("unexpected language %v", body["language"])
	}
	if loadSettings(t, path).Language != "en-US" {
		t.Errorf("the language was not written to the configuration")
	}
	if i18n.Get() != "en-US" {
		t.Errorf("the language in use is %q", i18n.Get())
	}
}

// TestSaveSettingsKeepsTheLanguage makes sure that a request without a
// language does not change the language in use.
func TestSaveSettingsKeepsTheLanguage(t *testing.T) {
	defer i18n.Set(i18n.Get())
	i18n.Set(i18n.EnUS)
	ts, _ := testServer(t)
	code, _ := post(t, ts, "/api/settings", map[string]any{"keepaliveSeconds": 45})
	if code != http.StatusOK {
		t.Fatalf("the settings are not saved: %d", code)
	}
	if i18n.Get() != i18n.EnUS {
		t.Errorf("the language changed to %q", i18n.Get())
	}
}

func TestInfoShowsTheLanguage(t *testing.T) {
	ts, _ := testServer(t)
	code, body := get(t, ts, "/api/info")
	if code != http.StatusOK {
		t.Fatalf("the info is not served: %d", code)
	}
	if body["language"] == nil {
		t.Errorf("the language is missing: %v", body)
	}
	if body["theme"] != config.ThemeAuto {
		t.Errorf("the theme is missing: %v", body)
	}
	locales, ok := body["locales"].([]any)
	if !ok || len(locales) == 0 {
		t.Errorf("the locales are missing: %v", body)
	}
}

func TestInfoShowsTheVersion(t *testing.T) {
	ts, _ := testServer(t)
	code, body := get(t, ts, "/api/info")
	if code != http.StatusOK {
		t.Fatalf("the info is not served: %d", code)
	}
	if body["version"] != version.Version {
		t.Errorf("unexpected version %v", body["version"])
	}
}

func TestOpenWebsite(t *testing.T) {
	old := openBrowser
	defer func() { openBrowser = old }()
	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	ts, _ := testServer(t)
	code, body := post(t, ts, "/api/open-website", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("the website is not opened: %d %v", code, body)
	}
	if opened != officialSite {
		t.Errorf("opened %q", opened)
	}
}

func TestSaveHostWithKeyText(t *testing.T) {
	ts, path := testServer(t)
	keyText := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n"
	code, body := post(t, ts, "/api/hosts", map[string]any{
		"name": "web",
		"host": "10.0.0.1",
		"port": 22,
		"auth": map[string]any{
			"type":    "key",
			"user":    "root",
			"keyPath": filepath.Join(t.TempDir(), "missing"),
			"keyText": keyText,
		},
	})
	if code != http.StatusOK {
		t.Fatalf("the host is not saved: %d %v", code, body)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Auth.KeyText != keyText {
		t.Fatalf("the key text was not stored: %+v", cfg.Hosts)
	}

	resp, err := http.Get(ts.URL + "/api/hosts")
	if err != nil {
		t.Fatalf("get hosts: %v", err)
	}
	defer resp.Body.Close()
	var hosts []hostView
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		t.Fatalf("decode hosts: %v", err)
	}
	if len(hosts) != 1 || hosts[0].KeyText != keyText {
		t.Fatalf("the key text is not served: %+v", hosts)
	}
}

func TestSaveHostDefaultsToPasswordAuth(t *testing.T) {
	ts, path := testServer(t)
	code, body := post(t, ts, "/api/hosts", map[string]any{
		"name": "web",
		"host": "10.0.0.1",
		"port": 22,
		"auth": map[string]any{"user": "root", "password": "secret"},
	})
	if code != http.StatusOK {
		t.Fatalf("the host is not saved: %d %v", code, body)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if len(cfg.Hosts) != 1 || cfg.Hosts[0].Auth.Type != config.AuthPassword {
		t.Fatalf("unexpected host auth: %+v", cfg.Hosts)
	}
}

func TestSaveHostRejectsDuplicate(t *testing.T) {
	ts, path := testServer(t)
	payload := map[string]any{
		"name": "web",
		"host": "Example.COM",
		"port": 22,
		"auth": map[string]any{"user": "root", "password": "secret"},
	}
	if code, body := post(t, ts, "/api/hosts", payload); code != http.StatusOK {
		t.Fatalf("the first host is not saved: %d %v", code, body)
	}

	payload["name"] = "web again"
	payload["host"] = "example.com"
	code, body := post(t, ts, "/api/hosts", payload)
	if code != http.StatusBadRequest {
		t.Fatalf("a duplicate host was accepted: %d %v", code, body)
	}
	if message, _ := body["error"].(string); message == "" {
		t.Error("a duplicate host did not return an error message")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if len(cfg.Hosts) != 1 {
		t.Fatalf("a duplicate host was stored: %+v", cfg.Hosts)
	}

	payload["name"] = "deploy"
	payload["auth"] = map[string]any{"user": "deploy", "password": "secret"}
	if code, body := post(t, ts, "/api/hosts", payload); code != http.StatusOK {
		t.Fatalf("a host with another user is not saved: %d %v", code, body)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if len(cfg.Hosts) != 2 {
		t.Fatalf("a host with another user was not stored: %+v", cfg.Hosts)
	}
}

func TestAutostartRefusesAnUnknownMechanism(t *testing.T) {
	ts, _ := testServer(t)
	code, _ := post(t, ts, "/api/autostart", map[string]any{"kind": "nope", "enabled": true})
	if code != http.StatusBadRequest {
		t.Fatalf("an unknown mechanism is accepted: %d", code)
	}
}

// TestIndexHasTheTargetFieldsAndHeaderLinks makes sure the tunnel form keeps
// the target address and port in separate fields, and that the header has the
// version tag and the official site link.
func TestIndexHasTheTargetFieldsAndHeaderLinks(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get the index: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}
	for _, id := range []string{`id="t_target_host"`, `id="t_target_port"`, `id="versiontag"`, `id="officialsite"`} {
		if !bytes.Contains(body, []byte(id)) {
			t.Errorf("the index is missing %s", id)
		}
	}
	if bytes.Contains(body, []byte(`id="t_target"`)) {
		t.Errorf("the tunnel form still has a single target field")
	}
}

// TestIndexHidesSingleAutostartMechanism makes sure the settings page can
// hide the mechanism selector when the platform offers only one choice.
func TestIndexHidesSingleAutostartMechanism(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get the index: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}
	if !bytes.Contains(body, []byte(`id="wrap_autokind"`)) {
		t.Error("the autostart mechanism field has no wrapper")
	}
	if !bytes.Contains(body, []byte(`mechanisms.length < 2`)) {
		t.Error("the autostart mechanism field is not hidden for a single choice")
	}
}
