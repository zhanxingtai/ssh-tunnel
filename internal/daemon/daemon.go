// Package daemon manages a background instance of the tool: it starts the
// process detached from the terminal and keeps its pid in the state
// directory so the process can be stopped again.
package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ssh-tunnel/internal/config"
	"ssh-tunnel/internal/i18n"
)

// PIDFileName is the name of the file holding the pid of the background
// instance.
const PIDFileName = "daemon.pid"

// StateDir is the directory holding the user state.
func StateDir() (string, error) {
	return config.DefaultDir()
}

// PIDFile returns the path of the pid file.
func PIDFile() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, PIDFileName), nil
}

// WritePID records the pid of the running background instance.
func WritePID() error {
	path, err := PIDFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return i18n.E("err.create_state_dir", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return i18n.E("err.write_pid_file", err)
	}
	return nil
}

// RemovePID deletes the pid file.
func RemovePID() {
	if path, err := PIDFile(); err == nil {
		os.Remove(path)
	}
}

// PID returns the pid recorded in the pid file.
func PID() (int, bool) {
	path, err := PIDFile()
	if err != nil {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// Running reports whether the background instance is alive.
func Running() (int, bool) {
	pid, ok := PID()
	if !ok {
		return 0, false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}

// Stop asks the background instance to stop and waits for it to exit.
func Stop() error {
	pid, ok := PID()
	if !ok {
		return i18n.E("err.no_instance")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		RemovePID()
		return i18n.E("err.locate_process", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		RemovePID()
		return i18n.E("err.stop_process", pid, err)
	}
	for i := 0; i < 50; i++ {
		if _, alive := Running(); !alive {
			RemovePID()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return i18n.E("err.process_not_stopped", pid)
}
