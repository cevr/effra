//go:build unix

package main

import (
	"os/exec"
	"syscall"
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
