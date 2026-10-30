package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// totalMemory reads hw.memsize, or returns 0.
func totalMemory() int64 {
	output, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0
	}
	return size
}
