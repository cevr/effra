//go:build linux

package producer

import (
	"io"
	"os"
)

// openImage opens the executing image through procfs, which names the file
// the kernel mapped rather than any installation pathname.
func openImage() (io.ReadCloser, error) { return os.Open("/proc/self/exe") }
