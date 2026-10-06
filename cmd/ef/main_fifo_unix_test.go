//go:build unix

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadRejectsFIFOBeforeOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.ef")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := load(path, "go")
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
			t.Fatalf("unexpected CLI FIFO admission result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("CLI source admission blocked before opening FIFO")
	}
}
