//go:build unix

package main

import "syscall"

// diagnosticsSmokeMkfifo creates a named pipe for the source admission bound.
func diagnosticsSmokeMkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
