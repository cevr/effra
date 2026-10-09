//go:build !unix

package main

// setCreationMask is a no-op where files have no Unix creation mask.
func setCreationMask() {}
