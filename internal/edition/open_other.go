//go:build !unix

package edition

import "os"

// SourceOpenFlags open a source read-only. Platforms without Unix FIFOs
// have no non-blocking open to ask for.
const SourceOpenFlags = os.O_RDONLY
