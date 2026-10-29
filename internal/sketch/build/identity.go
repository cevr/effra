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
// SHA-256 of its executable's bytes. Every byte compiled into the compiler
// (the emitter, the embedded standard library and runtime, a dirty edit)
// reaches the emission key through it, and two processes of the same
// executable share it, so unchanged builds reuse modules across processes
// on every platform. A process nonce never keys a published module: when
// the executable cannot be read, building fails.
//
// The executable is opened when the process starts (startImage) and hashed
// from that open file on the first call. On Linux the file is
// /proc/self/exe, the image the process runs. Elsewhere it is the file
// os.Executable names at start, so a reinstall that renames another
// compiler over that path later in the process (go install, a package
// manager) cannot make this process's output be published under the new
// compiler's key: the open file is still the old inode. A replacement
// between exec and package initialisation is not covered.
//
// Prior art: Go keys its action cache on the build IDs of the compiler and
// tools it runs, read from the files on disk when they run; Gleam and Mix
// key on a compiler version string, which a dirty build does not change.
func CompilerIdentity() (string, error) { return compilerIdentity() }

// startImage is the running executable, opened during package
// initialisation, before any source is read; startErr is why it could not
// be. The file stays open for the life of the process.
var startImage, startErr = openExecutable(runtime.GOOS)

var compilerIdentity = sync.OnceValues(func() (string, error) {
	if startErr != nil {
		return "", identityError(startErr)
	}
	return imageIdentity(startImage)
})

// openExecutable opens the running executable as goos finds it:
// /proc/self/exe on Linux, else the file os.Executable names.
func openExecutable(goos string) (*os.File, error) {
	if goos == "linux" {
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

// imageIdentity digests all bytes of image from offset 0, whatever its
// current offset.
func imageIdentity(image io.ReaderAt) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(image, 0, 1<<62)); err != nil {
		return "", identityError(err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func identityError(err error) error {
	return fmt.Errorf("build: the compiler executable cannot be read, so its output cannot be cached: %w", err)
}
