//go:build !unix

package main

import (
	"errors"
	"runtime"
)

func openRSSWatch(memoryBudget, string) (limiter, error) {
	return nil, errors.New("memory budget: no mechanism on " + runtime.GOOS)
}
