//go:build darwin

package edition

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

// descriptorPathSource names how this platform finds an open file's path.
const descriptorPathSource = "fcntl F_GETPATH"

// descriptorPath returns the path the kernel records for the open file,
// through fcntl(F_GETPATH). The buffer is MAXPATHLEN (1024) bytes.
func descriptorPath(file *os.File) (path string, ok bool, err error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return "", false, err
	}
	var buffer [1024]byte
	var errno syscall.Errno
	if err := raw.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETPATH, uintptr(unsafe.Pointer(&buffer[0])))
	}); err != nil {
		return "", false, err
	}
	if errno != 0 {
		return "", false, errno
	}
	end := bytes.IndexByte(buffer[:], 0)
	if end < 0 {
		end = len(buffer)
	}
	return string(buffer[:end]), true, nil
}
