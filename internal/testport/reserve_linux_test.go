//go:build linux

package testport

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// A reserved address admits one listener at a time: a server can listen on
// it, a second listener is refused while the first lives, and the address
// can be bound again once the first is closed.
func TestReservedAddressAdmitsOneListenerAtATime(t *testing.T) {
	address := Reserve(t)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listening on the reserved address: %v", err)
	}
	if second, err := net.Listen("tcp", address); !errors.Is(err, syscall.EADDRINUSE) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("a second listener on a live reserved address: %v, want EADDRINUSE", err)
	}
	listener.Close()
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listening again after release: %v", err)
	}
	listener.Close()
}
