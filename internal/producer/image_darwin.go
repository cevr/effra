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
// carries that cdhash, its pages match it, and the file ends where its
// signature ends (verifyCodeSignature): a universal file or a byte after the
// signature gives a process-scoped identity, because the digest would name
// bytes the kernel never checked. The file is read once, so the bytes
// verified are the bytes hashed. A Go binary for
// darwin/arm64 is always signed (the linker signs it ad hoc); an unsigned
// image has no kernel record, and its identity stays process-scoped.
//
// The design relies on xnu answering CS_OPS_CDHASH from the code signature
// blob attached to the process's text vnode, which the kernel keeps while
// that vnode is in use (inferred from xnu's csops and vn_getcdhash, not
// measured directly). Consistent with that, measured on darwin/arm64: a
// signed file rewritten in place and run stays killed after its bytes are
// restored, while a new file with the same bytes runs. Rewriting the file in
// place to another signed build while an old process runs is not measured.
//
// csops is reached by its raw trap number; Go's own darwin packages call
// libSystem instead. A refused trap gives the process-scoped fallback.
func openImage() (io.ReadCloser, error) {
	var cdhash [cdhashSize]byte
	if _, _, errno := syscall.Syscall6(syscall.SYS_CSOPS, uintptr(os.Getpid()), csOpsCDHash, uintptr(unsafe.Pointer(&cdhash[0])), cdhashSize, 0, 0); errno != 0 {
		return nil, errNoSignatureRecord
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
