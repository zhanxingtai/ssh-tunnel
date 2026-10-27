//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

import "ssh-tunnel/internal/i18n"

// detachedProcess and createNewProcessGroup detach the child from the
// console of the parent.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// Start launches the tool detached from the console and waits until it
// recorded its pid.
func Start(exe string, args ...string) error {
	if _, alive := Running(); alive {
		return i18n.E("err.instance_running")
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
	if err := cmd.Start(); err != nil {
		return i18n.E("err.start_instance", err)
	}
	pid := cmd.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	for i := 0; i < 50; i++ {
		if recorded, ok := PID(); ok && recorded == pid {
			return nil
		}
		select {
		case err := <-exited:
			return i18n.E("err.instance_exited", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	return i18n.E("err.instance_not_started", pid)
}

// Exists reports whether the executable is there.
func Exists(exe string) bool {
	_, err := os.Stat(exe)
	return err == nil
}
