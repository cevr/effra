// Package source owns the shared source-file admission used by CLI and MCP.
package source

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrTooLarge identifies a source that exceeded the caller's configured bound.
// Callers own the user-facing wording for that bound.
var ErrTooLarge = errors.New("source exceeds configured limit")

// ReadRegularFile admits a regular source node before opening it, then checks
// the opened descriptor again before reading. A zero maxBytes means that the
// caller has no source-size bound.
func ReadRegularFile(path string, maxBytes int) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("source size limit must not be negative")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	descriptorInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !descriptorInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular file")
	}

	if maxBytes <= 0 {
		return io.ReadAll(file)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}
