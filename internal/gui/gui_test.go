package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/i18n"
	"ssh-tunnel/internal/logging"
	"ssh-tunnel/internal/manager"
	"ssh-tunnel/internal/version"
)

// TestMain keeps the tests away from the keyring of the system: the
// credentials of the tests stay in the encrypted file of the test directory.
func TestMain(m *testing.M) {
	os.Setenv("SSH_TUNNEL_KEYRING", "0")
	os.Exit(m.Run())
}

// testAPI is a running server of the interface.
type testAPI struct {
	*httptest.Server
	// token authorizes the requests of the test.
	token string
}

// testServer starts a server on a temporary configuration file.
func testServer(t *testing.T) (*testAPI, string) {
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
	ts := &testAPI{Server: httptest.NewServer(handler), token: srv.token}
	t.Cleanup(ts.Close)
	return ts, path
}

// request builds a request that carries the token of the session.
func (a *testAPI) request(method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, a.URL+path, body)
	if err != nil {
		panic(err)
	}
	req.Header.Set(tokenHeader, a.token)
	return req
}

// get fetches a path of the API.
func get(t *testing.T, ts *testAPI, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.DefaultClient.Do(ts.request(http.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// getRaw fetches a path of the API and returns the answer as it is.
func getRaw(t *testing.T, ts *testAPI, path string) (int, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(ts.request(http.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, data
}

// post sends a payload to a path of the API.
func post(t *testing.T, ts *testAPI, path string, payload any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode the payload: %v", err)
	}
	resp, err := http.DefaultClient.Do(ts.request(http.MethodPost, path, bytes.NewReader(data)))
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

// TestSaveHostWithKeyText checks that the private key is kept in the secret
// store and that the interface never receives it.
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

	// The configuration file must not hold the private key.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration file: %v", err)
	}
	if bytes.Contains(data, []byte(keyText)) {
		t.Error("the configuration file holds the private key")
	}

	// The interface must not receive the private key.
	_, hosts := getRaw(t, ts, "/api/hosts")
	if bytes.Contains(hosts, []byte(keyText)) {
		t.Error("the interface is served the private key")
	}
	if !bytes.Contains(hosts, []byte(`"hasKeyText":true`)) {
		t.Errorf("the interface does not know that a private key is stored: %s", hosts)
	}
}

// TestSaveHostKeepsTheStoredCredentials checks that a host that is saved
// without credentials keeps the ones it already had.
func TestSaveHostKeepsTheStoredCredentials(t *testing.T) {
	ts, path := testServer(t)
	code, body := post(t, ts, "/api/hosts", map[string]any{
		"name": "web",
		"host": "10.0.0.1",
		"port": 22,
		"auth": map[string]any{"type": "password", "user": "root", "password": "s3cret"},
	})
	if code != http.StatusOK {
		t.Fatalf("the host is not saved: %d %v", code, body)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	id := cfg.Hosts[0].ID

	// The host is saved again without a password.
	code, body = post(t, ts, "/api/hosts", map[string]any{
		"id":   id,
		"name": "web",
		"host": "10.0.0.1",
		"port": 22,
		"auth": map[string]any{"type": "password", "user": "root"},
	})
	if code != http.StatusOK {
		t.Fatalf("the host is not saved: %d %v", code, body)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if cfg.Hosts[0].Auth.Password != "s3cret" {
		t.Fatalf("the password was lost: %+v", cfg.Hosts[0].Auth)
	}
}

// TestTheApiRefusesARequestWithoutTheToken checks that the interface of the
// program cannot be driven by another program of the machine.
func TestTheApiRefusesARequestWithoutTheToken(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/api/hosts")
	if err != nil {
		t.Fatalf("get hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request without the token was accepted: %d", resp.StatusCode)
	}
}

// TestTheTokenIsAcceptedInTheAddress checks that the page of the program can
// be opened with the token in its address.
func TestTheTokenIsAcceptedInTheAddress(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/api/hosts?token=" + ts.token)
	if err != nil {
		t.Fatalf("get hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the token of the address was refused: %d", resp.StatusCode)
	}
}

// TestTheTokenIsKeptInACookie checks that the program gives the token to the
// browser, so that the page keeps it when its address changes.
func TestTheTokenIsKeptInACookie(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/?token=" + ts.token)
	if err != nil {
		t.Fatalf("get the index: %v", err)
	}
	defer resp.Body.Close()
	cookie := resp.Cookies()
	if len(cookie) != 1 || cookie[0].Name != tokenCookie || cookie[0].Value != ts.token {
		t.Fatalf("the token was not given to the browser: %+v", cookie)
	}
}

// TestAnUnknownTokenGetsNoCookie makes sure that a page of another site
// cannot make the browser keep the token of the program.
func TestAnUnknownTokenGetsNoCookie(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/?token=guess")
	if err != nil {
		t.Fatalf("get the index: %v", err)
	}
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == tokenCookie {
			t.Fatal("the token was given to a request that did not know it")
		}
	}
}

// TestTheApiRefusesAReboundHost checks that the origin is not compared with
// the host of the request: a page of another site could pretend to be the
// server.
func TestTheApiRefusesAReboundHost(t *testing.T) {
	ts, _ := testServer(t)
	req := ts.request(http.MethodPost, "/api/hosts", bytes.NewReader([]byte("{}")))
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request with a rebound host was accepted: %d", resp.StatusCode)
	}
}

// TestTheApiRefusesAnotherOrigin checks that a page of another site cannot
// drive the program.
func TestTheApiRefusesAnotherOrigin(t *testing.T) {
	ts, _ := testServer(t)
	req := ts.request(http.MethodPost, "/api/hosts", bytes.NewReader([]byte("{}")))
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post hosts: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a request from another origin was accepted: %d", resp.StatusCode)
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

// TestThePageDoesNotLeakTheAddress makes sure that the address of the
// server, which carries the token of the session, is not sent to another
// site.
func TestThePageDoesNotLeakTheAddress(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get the index: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("unexpected referrer policy %q", got)
	}
}

// TestThePageTranslationsHaveTheSameKeys makes sure that the two
// dictionaries of the page hold the same messages.
func TestThePageTranslationsHaveTheSameKeys(t *testing.T) {
	data, err := assets.ReadFile("assets/i18n.js")
	if err != nil {
		t.Fatalf("read the translations: %v", err)
	}
	zh := translationKeys(t, string(data), "zh-CN")
	en := translationKeys(t, string(data), "en-US")
	for key := range zh {
		if !en[key] {
			t.Errorf("the English translation misses %q", key)
		}
	}
	for key := range en {
		if !zh[key] {
			t.Errorf("the Chinese translation misses %q", key)
		}
	}
}

// translationKeys returns the message keys of one locale of the page.
func translationKeys(t *testing.T, content, locale string) map[string]bool {
	t.Helper()
	marker := "'" + locale + "': {"
	start := strings.Index(content, marker)
	if start < 0 {
		t.Fatalf("the locale %s is missing", locale)
	}
	rest := content[start+len(marker):]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("the locale %s is not closed", locale)
	}
	keys := map[string]bool{}
	for _, match := range regexp.MustCompile(`'([^']+)':`).FindAllStringSubmatch(rest[:end], -1) {
		keys[match[1]] = true
	}
	return keys
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
