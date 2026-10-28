//go:build unix

package source

import (
	"os"
	"syscall"
)

// OpenFlags open a source read-only without blocking: opening a FIFO returns
// at once, and Open then refuses it as not a regular file. Reads of a
// regular file are unaffected by O_NONBLOCK.
const OpenFlags = os.O_RDONLY | syscall.O_NONBLOCK
