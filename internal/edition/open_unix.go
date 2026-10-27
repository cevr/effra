//go:build unix

package edition

import (
	"os"
	"syscall"
)

// SourceOpenFlags open a source read-only without blocking: opening a FIFO
// returns at once, and ResolveFile then refuses it as not a regular file.
// Reads of a regular file are unaffected by O_NONBLOCK.
const SourceOpenFlags = os.O_RDONLY | syscall.O_NONBLOCK
