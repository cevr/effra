//go:build unix

package source

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadRegularFileRejectsFIFOBeforeOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.ef")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := ReadRegularFile(path, 0)
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
			t.Fatalf("unexpected FIFO admission result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("regular-file admission blocked before opening FIFO")
	}
}
