//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a new session, outside the pack's process group.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
