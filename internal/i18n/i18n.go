// Package i18n holds the message catalogs of the program and the locale
// currently in use.
//
// The locale is chosen in this order: the --lang option of the command
// line, the SSH_TUNNEL_LANG environment variable, the language of the system
// and finally the default one. The locale can be changed while the program
// runs, which is what the language selector of the interface does.
package i18n

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// The supported locales.
const (
	// ZhCN is Simplified Chinese.
	ZhCN = "zh-CN"
	// EnUS is English.
	EnUS = "en-US"
)

// Default is the locale used when nothing else is known.
const Default = ZhCN

// EnvVar is the environment variable that overrides the locale.
const EnvVar = "SSH_TUNNEL_LANG"

var (
	mu      sync.RWMutex
	current = Default
)

// Locales returns the supported locales.
func Locales() []string { return []string{ZhCN, EnUS} }

// Name returns the name of a locale, written in that locale.
func Name(locale string) string {
	switch Normalize(locale) {
	case ZhCN:
		return "简体中文"
	case EnUS:
		return "English"
	default:
		return locale
	}
}

// Normalize maps the many spellings of a locale onto the supported ones. It
// returns an empty string when the locale is not supported.
func Normalize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Drop the encoding and the modifier: "zh_CN.UTF-8" and "zh_CN@pinyin"
	// both become "zh_cn".
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	lower := strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	switch {
	case lower == "zh" || strings.HasPrefix(lower, "zh_") || strings.HasPrefix(lower, "zh"):
		return ZhCN
	case lower == "en" || strings.HasPrefix(lower, "en_") || strings.HasPrefix(lower, "en"):
		return EnUS
	default:
		return ""
	}
}

// Detect returns the locale of the environment.
func Detect() string {
	for _, key := range []string{EnvVar, "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			if l := Normalize(v); l != "" {
				return l
			}
		}
	}
	return Default
}

// Set switches the locale in use. An unsupported locale falls back to the
// default one.
func Set(locale string) {
	l := Normalize(locale)
	if l == "" {
		l = Default
	}
	mu.Lock()
	current = l
	mu.Unlock()
}

// Get returns the locale in use.
func Get() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the message of the given key in the current locale. The optional
// arguments fill the placeholders of the message.
func T(key string, args ...any) string {
	return F(key, args...)
}

// E returns the error of the given key in the current locale. The optional
// arguments fill the placeholders of the message; an error passed as the last
// argument is wrapped by the message.
func E(key string, args ...any) error {
	msg := lookup(key)
	if len(args) == 0 {
		return errors.New(msg)
	}
	return fmt.Errorf(msg, args...)
}

// F returns the formatted message of the given key in the current locale.
//
// A message that wraps an error (%w) is formatted with %v: the wrapped error
// is only kept by E.
func F(key string, args ...any) string {
	msg := lookup(key)
	if len(args) == 0 {
		return msg
	}
	if strings.Contains(msg, "%w") {
		msg = strings.ReplaceAll(msg, "%w", "%v")
	}
	return fmt.Sprintf(msg, args...)
}

// lookup returns the message of a key in the current locale, falling back to
// the default locale and finally to the key itself.
func lookup(key string) string {
	mu.RLock()
	locale := current
	mu.RUnlock()

	msg, ok := catalogs[locale][key]
	if !ok {
		msg, ok = catalogs[Default][key]
	}
	if !ok {
		msg = key
	}
	return msg
}
