//go:build !unix

package source

import "os"

// OpenFlags open a source read-only. Platforms without Unix FIFOs have no
// non-blocking open to ask for.
const OpenFlags = os.O_RDONLY
