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
	address, reserved := TryReserve(t)
	if !reserved {
		t.Fatal("no reservation on Linux")
	}
	listen := func(what string) net.Listener {
		t.Helper()
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		t.Cleanup(func() { listener.Close() })
		return listener
	}
	first := listen("listening on the reserved address")
	if second, err := net.Listen("tcp", address); !errors.Is(err, syscall.EADDRINUSE) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("a second listener on a live reserved address: %v, want EADDRINUSE", err)
	}
	first.Close()
	listen("listening again after release")
}
