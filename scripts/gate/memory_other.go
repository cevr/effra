//go:build !linux && !darwin

package main

// totalMemory is unknown here: the gate budget must be set
// (gateMemoryVariable).
func totalMemory() int64 { return 0 }
