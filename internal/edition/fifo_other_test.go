//go:build !unix

package edition

import "errors"

func makeFIFO(string) error { return errors.New("no FIFOs on this platform") }
