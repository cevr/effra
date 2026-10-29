package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
)

// CompilerIdentity is the content identity of the running compiler: the
// SHA-256 of its executable's bytes, read once per process. Every byte
// compiled into the compiler (the emitter, the embedded standard library
// and runtime, a dirty edit) reaches the emission key through it, and two
// processes of the same executable share it, so unchanged builds reuse
// modules across processes on every platform. A process nonce never keys
// a published module: when the executable cannot be read, building fails.
//
// On Linux the bytes come from /proc/self/exe, the image the process runs
// even if its installed path was replaced since; elsewhere from the file
// os.Executable names.
//
// Prior art: Go keys its action cache on the build IDs of the compiler and
// tools it runs; Gleam and Mix key on a compiler version string, which a
// dirty build does not change.
func CompilerIdentity() (string, error) { return compilerIdentity() }

var compilerIdentity = sync.OnceValues(func() (string, error) { return executableIdentity(openExecutable) })

// openExecutable opens the running executable's bytes.
func openExecutable() (io.ReadCloser, error) {
	if runtime.GOOS == "linux" {
		if f, err := os.Open("/proc/self/exe"); err == nil {
			return f, nil
		}
	}
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// executableIdentity digests the bytes open returns.
func executableIdentity(open func() (io.ReadCloser, error)) (string, error) {
	image, err := open()
	if err != nil {
		return "", fmt.Errorf("build: the compiler executable cannot be read, so its output cannot be cached: %w", err)
	}
	defer image.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, image); err != nil {
		return "", fmt.Errorf("build: the compiler executable cannot be read, so its output cannot be cached: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
