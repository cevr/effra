//go:build !unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// isolateQuery leaves a query in the gate's process group where Unix
// process groups do not exist; WaitDelay still bounds it.
func isolateQuery(command *exec.Cmd) {}

func endQuery(command *exec.Cmd) {}

// stopSignals are the signals that end the gate.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// reraise ends the gate as an interrupt would.
func reraise(caught os.Signal) { os.Exit(130) }
