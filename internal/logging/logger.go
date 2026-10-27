// Package logging provides a small logger that writes to a file, keeps the
// most recent lines in memory and notifies subscribers in real time.
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

import "ssh-tunnel/internal/i18n"

// Logger writes timestamped lines to a file and to an in-memory ring buffer.
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	ring    []string
	maxRing int
	subs    map[chan string]struct{}
}

// New creates a logger writing to path. An empty path disables file output.
func New(path string) (*Logger, error) {
	l := &Logger{maxRing: 500, subs: map[chan string]struct{}{}}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, i18n.E("err.create_log_dir", err)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, i18n.E("err.open_log_file", err)
		}
		l.file = f
	}
	return l, nil
}

// Printf records a line.
func (l *Logger) Printf(format string, args ...any) {
	line := fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))

	l.mu.Lock()
	l.ring = append(l.ring, line)
	if len(l.ring) > l.maxRing {
		l.ring = l.ring[len(l.ring)-l.maxRing:]
	}
	if l.file != nil {
		fmt.Fprintln(l.file, line)
	}
	subs := make([]chan string, 0, len(l.subs))
	for ch := range l.subs {
		subs = append(subs, ch)
	}
	l.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Lines returns a copy of the buffered lines.
func (l *Logger) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.ring))
	copy(out, l.ring)
	return out
}

// Subscribe returns a channel receiving new log lines and a cancel function.
func (l *Logger) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 256)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

// Close releases the log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
