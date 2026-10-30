//go:build darwin

package producer

import (
	"bytes"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// csOpsCDHash asks csops(2) for the cdhash of a process's main executable,
// as xnu's cs_blobs.h numbers it.
const csOpsCDHash = 5

// openImage returns the bytes of the executing image. Darwin has no
// /proc/self/exe, and os.Executable names the path the process was started
// from, which may since name other bytes. The kernel keeps the cdhash of the
// image it executes; the file at that path counts only if its code signature
// carries that cdhash and its pages match it (verifyCodeSignature). The file
// is read once, so the bytes verified are the bytes hashed. A Go binary for
// darwin/arm64 is always signed (the linker signs it ad hoc); an unsigned
// image has no kernel record, and its identity stays process-scoped.
func openImage() (io.ReadCloser, error) {
	var cdhash [cdhashSize]byte
	if _, _, errno := syscall.Syscall6(syscall.SYS_CSOPS, uintptr(os.Getpid()), csOpsCDHash, uintptr(unsafe.Pointer(&cdhash[0])), cdhashSize, 0, 0); errno != 0 {
		return nil, errUnverified
	}
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	image, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(image) > maxImageBytes {
		return nil, errImageTooLarge
	}
	if err := verifyCodeSignature(image, cdhash[:]); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(image)), nil
}
