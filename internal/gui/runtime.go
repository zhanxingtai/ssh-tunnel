package gui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RuntimeInfo describes a running background instance so that a second
// process can hand over to it instead of starting a duplicate.
type RuntimeInfo struct {
	URL       string    `json:"url"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	// Window tells whether the running instance shows a native window.
	Window bool `json:"window"`
}

// RuntimeFile returns the path of the runtime information file next to the
// configuration file.
func RuntimeFile(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "runtime.json")
}

// WriteRuntimeFile records the address of the running instance.
func WriteRuntimeFile(path string, info RuntimeInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode runtime info: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write runtime info: %w", err)
	}
	return nil
}

// ReadRuntimeFile returns the address of the running instance. A missing file
// is not an error: it simply means that no instance is running.
func ReadRuntimeFile(path string) (RuntimeInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeInfo{}, false
	}
	var info RuntimeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return RuntimeInfo{}, false
	}
	return info, true
}

// RemoveRuntimeFile deletes the runtime information file.
func RemoveRuntimeFile(path string) {
	os.Remove(path)
}
