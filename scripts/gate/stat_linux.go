//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// changeIdentity names a file by device, inode, size and status-change time.
// Every write to the file advances its status-change time, and no caller can
// set it back, so a digest recorded under this identity is still the file's.
func changeIdentity(info os.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d.%09d", stat.Dev, stat.Ino, stat.Size, stat.Ctim.Sec, stat.Ctim.Nsec), true
}
