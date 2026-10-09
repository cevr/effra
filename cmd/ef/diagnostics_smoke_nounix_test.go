//go:build !unix

package main

import "errors"

// diagnosticsSmokeMkfifo reports that this platform has no named pipes, so
// the source admission bound omits its FIFO case.
func diagnosticsSmokeMkfifo(string) error {
	return errors.ErrUnsupported
}
