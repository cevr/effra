//go:build !linux

package main

import "os"

// changeIdentity is unavailable here, so every executable is hashed each run.
func changeIdentity(os.FileInfo) (string, bool) { return "", false }
