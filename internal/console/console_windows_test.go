//go:build windows

package console

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteKeepsUTF8InFiles makes sure that redirected output stays UTF-8:
// only a real console is written as UTF-16.
func TestWriteKeepsUTF8InFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	const message = "中文与 English\n"
	write(file, message)
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != message {
		t.Fatalf("unexpected output %q", data)
	}
}
