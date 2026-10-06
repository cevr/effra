package effra

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

// File is a managed handle. Closing its scope invalidates subsequent operations.
type File struct {
	mu     sync.Mutex
	native *os.File
	closed bool
}

func OpenRead(path string) Effect[*File] {
	return AcquireRelease(path, func(context.Context) (*File, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &File{native: file}, nil
	}, func(file *File, _ context.Context) error {
		file.mu.Lock()
		defer file.mu.Unlock()
		file.closed = true
		return file.native.Close()
	})
}
func ReadText(file *File) Effect[string] {
	return func(fc *FiberContext) Exit[string] {
		if file == nil {
			return Fail[string]("IoError", fmt.Errorf("nil file handle"))
		}
		file.mu.Lock()
		defer file.mu.Unlock()
		if file.closed {
			return Fail[string]("IoError", fmt.Errorf("file owner is closed"))
		}
		if err := fc.Checkpoint(); err != nil {
			return Interrupt[string](err)
		}
		data, err := io.ReadAll(file.native)
		if err != nil {
			return Fail[string]("IoError", err)
		}
		return Succeed(string(data))
	}
}
func ReadFile(path string) Effect[string] {
	return Scoped(func(fc *FiberContext) Exit[string] {
		file := Invoke(fc, OpenRead(path))
		if file.IsFailure() {
			return Propagate[string](file)
		}
		return Invoke(fc, ReadText(file.Value))
	})
}
