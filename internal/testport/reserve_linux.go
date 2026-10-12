//go:build linux

// Package testport gives a test a loopback address that no other socket on
// the host can be assigned until the test ends.
//
// A test learns a server's address by letting the server bind port 0 and
// reading the address it reports, as net/http/httptest does. Reserve is for
// the two cases that cannot work that way: a program that must be told its
// address before it starts, and a test that binds an address again after
// the server released it to prove the release. Closing a probe listener and
// handing its port over instead, or rebinding a released port, leaves a
// window in which another test can take the port (#112).
package testport

import (
	"net"
	"strconv"
	"syscall"
	"testing"
)

// Reserve returns a 127.0.0.1 address whose port stays reserved until the
// test ends. The test holds a socket bound to it, never listening, with
// SO_REUSEADDR. Linux then never hands the port to a socket that asks for
// any port: not to a bind to port 0 (inet_csk_find_open_port tests
// conflicts without the SO_REUSEADDR relaxation), and not to an outgoing
// connection's implicit bind (__inet_hash_connect skips every port that
// bind(2) holds). It still lets a server bind the address and listen on it,
// and lets the test bind it again once the server has released it, because
// Go, Node (libuv) and Bun (uSockets) set SO_REUSEADDR, and such sockets may
// share an address while at most one of them listens. A second listener on
// the address is refused as before, so a rebind still detects a listener
// that was not released.
func Reserve(t testing.TB) string {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(name.(*syscall.SockaddrInet4).Port))
}
