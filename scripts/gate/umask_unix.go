//go:build unix

package main

import "syscall"

// setCreationMask fixes the conventional file creation mask.
func setCreationMask() { syscall.Umask(0o022) }
