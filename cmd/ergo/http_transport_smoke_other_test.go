//go:build !unix

package main

import (
	"net"
	"testing"
)

// httpTransportSmokeAnswered needs a non-blocking MSG_PEEK on the socket,
// which only Unix exposes; elsewhere the admission and shutdown cases that
// depend on it are skipped.
func httpTransportSmokeAnswered(t *testing.T, _ net.Conn) bool {
	t.Helper()
	t.Skip("the admission-slot probe needs a non-blocking MSG_PEEK, which requires a Unix socket API")
	return false
}
