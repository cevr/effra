//go:build !linux

package main

import (
	"errors"
	"runtime"
)

// enterScope cannot run the gate in a systemd scope off Linux.
func enterScope(memoryBudget) string { return "no systemd on " + runtime.GOOS }

func openScope(string, memoryBudget) (limiter, error) {
	return nil, errors.New("no systemd on " + runtime.GOOS)
}
