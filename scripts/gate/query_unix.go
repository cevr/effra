//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// isolateQuery starts a tool query as the leader of its own process group,
// and makes cancelling it kill the whole group.
func isolateQuery(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

// endQuery kills what is left of a finished query's process group. The
// group outlives its reaped leader while any member remains, and its ID is
// not reused while it does.
func endQuery(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

// reraise gives a caught signal its default effect.
func reraise(caught os.Signal) {
	if number, ok := caught.(syscall.Signal); ok {
		_ = syscall.Kill(os.Getpid(), number)
		// Delivery to this process is asynchronous.
		time.Sleep(time.Second)
	}
	os.Exit(130)
}
