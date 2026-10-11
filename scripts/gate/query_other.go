//go:build !unix

package main

import "os/exec"

// isolateQuery leaves a query in the gate's process group where Unix
// process groups do not exist; WaitDelay still bounds it.
func isolateQuery(command *exec.Cmd) {}

func endQuery(command *exec.Cmd) {}
