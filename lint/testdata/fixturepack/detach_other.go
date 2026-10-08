//go:build !unix

package main

import "os/exec"

// detach has no process-group meaning here; the modes that use it run in
// Unix-only tests.
func detach(cmd *exec.Cmd) {}
