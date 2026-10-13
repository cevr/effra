//go:build !linux

package testport

import "testing"

// reserve reports that no reservation is possible off Linux (see the
// package comment).
func reserve(testing.TB) (string, bool) { return "", false }
