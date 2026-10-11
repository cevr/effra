//go:build !(linux && (amd64 || arm64))

package testinputs

import (
	"errors"
	"runtime"
)

// observed is never set: no process here is traced.
var observed bool

// observe reports that this platform cannot observe, so Main runs the tests
// unobserved and leaves the result uncached.
func observe(string) (int, *recorder, error) {
	return 0, nil, errors.New("no observation on " + runtime.GOOS + "/" + runtime.GOARCH)
}

// Without observation the recorder never runs; these answers keep it from
// remembering anything.

func writable(string) bool { return true }

func privileged() bool { return true }

func foreignDir(string) bool { return false }

func physicalDir(string) (string, bool) { return "", false }
