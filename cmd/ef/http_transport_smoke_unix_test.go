//go:build unix

package main

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// httpTransportSmokeAnswered reports, without waiting, whether the connection
// is readable: response bytes, end of stream or an error are pending.
func httpTransportSmokeAnswered(t *testing.T, connection net.Conn) bool {
	t.Helper()
	raw, err := connection.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	answered := false
	peek := make([]byte, 1)
	if err := raw.Read(func(fd uintptr) bool {
		_, _, err := syscall.Recvfrom(int(fd), peek, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		answered = !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return answered
}
