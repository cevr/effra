//go:build !linux && !darwin

package edition

import "os"

// descriptorPathSource names how this platform finds an open file's path.
const descriptorPathSource = "filepath.EvalSymlinks fallback"

// descriptorPath has no kernel query on this platform: admission falls
// back to resolving the spelling with filepath.EvalSymlinks and checking
// that the result names the open file (os.SameFile).
func descriptorPath(*os.File) (path string, ok bool, err error) {
	return "", false, nil
}
