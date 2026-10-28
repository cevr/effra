//go:build linux

package source

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
)

// descriptorPath returns the path the kernel records for the open file:
// the target of /proc/self/fd/N. ok is false only when /proc is not
// mounted, and the caller then falls back to resolving the spelling.
func descriptorPath(file *os.File) (path string, ok bool, err error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return "", false, err
	}
	var target string
	var readErr error
	if err := raw.Control(func(fd uintptr) {
		target, readErr = os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(fd), 10))
	}); err != nil {
		return "", false, err
	}
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			if _, err := os.Stat("/proc/self/fd"); errors.Is(err, fs.ErrNotExist) {
				return "", false, nil
			}
		}
		return "", false, readErr
	}
	return target, true, nil
}
