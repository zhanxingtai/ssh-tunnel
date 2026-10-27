package i18n

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"zh":          ZhCN,
		"zh-CN":       ZhCN,
		"zh_CN":       ZhCN,
		"zh_CN.UTF-8": ZhCN,
		"en":          EnUS,
		"en_US.UTF-8": EnUS,
		"en-GB":       EnUS,
		"fr":          "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTFollowsTheLocale(t *testing.T) {
	defer Set(Get())
	Set(EnUS)
	if got := T("tray.quit"); got != "Quit" {
		t.Errorf("T in english = %q", got)
	}
	Set(ZhCN)
	if got := T("tray.quit"); got != "退出" {
		t.Errorf("T in chinese = %q", got)
	}
}

func TestTFormatsTheArguments(t *testing.T) {
	defer Set(Get())
	Set(EnUS)
	if got := T("err.host_not_found", "web"); got != `host "web" not found` {
		t.Errorf("T with arguments = %q", got)
	}
}

func TestTUnknownKey(t *testing.T) {
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("T of an unknown key = %q", got)
	}
}

// TestTHandlesAWrappedError makes sure that a message written for E can also
// be shown as plain text.
func TestTHandlesAWrappedError(t *testing.T) {
	defer Set(Get())
	Set(EnUS)
	got := T("err.cannot_listen", "127.0.0.1:80", errors.New("in use"))
	if strings.Contains(got, "%!") {
		t.Errorf("T of a message with %%w = %q", got)
	}
	if !strings.Contains(got, "in use") {
		t.Errorf("T of a message with %%w = %q", got)
	}
}

// TestEKeepsTheWrappedError makes sure that E wraps the error it is given.
func TestEKeepsTheWrappedError(t *testing.T) {
	defer Set(Get())
	Set(EnUS)
	cause := errors.New("in use")
	err := E("err.cannot_listen", "127.0.0.1:80", cause)
	if !errors.Is(err, cause) {
		t.Errorf("E does not wrap the error: %v", err)
	}
}

// TestCatalogsHaveTheSameKeys makes sure that no message is missing in one of
// the locales: a missing key would silently show the other language.
func TestCatalogsHaveTheSameKeys(t *testing.T) {
	for _, locale := range Locales() {
		for key := range catalogs[Default] {
			if _, ok := catalogs[locale][key]; !ok {
				t.Errorf("the %s catalog is missing %q", locale, key)
			}
		}
	}
}

// TestCatalogsAreNotEmpty makes sure that every message has a translation.
func TestCatalogsAreNotEmpty(t *testing.T) {
	for locale, c := range catalogs {
		for key, msg := range c {
			if msg == "" {
				t.Errorf("the %s catalog has an empty message for %q", locale, key)
			}
		}
	}
}
