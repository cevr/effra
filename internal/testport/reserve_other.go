//go:build !linux

// Package testport gives a test a loopback address that no other socket on
// the host can be assigned until the test ends. See reserve_linux.go.
package testport

import "testing"

// Reserve skips the test off Linux. The reservation depends on Linux letting
// a listener share an address with a bound, non-listening SO_REUSEADDR
// socket. BSD kernels refuse that bind unless both sockets set SO_REUSEPORT,
// and the runtimes do not set it. Closing a probe listener and handing its
// port over instead would bring back the race of #112.
func Reserve(t testing.TB) string {
	t.Helper()
	t.Skip("reserving a loopback port needs Linux bind semantics")
	return ""
}
