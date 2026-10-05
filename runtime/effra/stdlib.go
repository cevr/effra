package effra

import (
	"context"
	"encoding/json"
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
func Env(name string) Effect[string] {
	return func(*FiberContext) Exit[string] { return Succeed(os.Getenv(name)) }
}
func Println(message string) Effect[Unit] {
	return func(*FiberContext) Exit[Unit] { fmt.Println(message); return Succeed(Unit{}) }
}
func InspectScope() Effect[string] {
	return func(fc *FiberContext) Exit[string] {
		data, err := json.Marshal(fc.Scope().Snapshot())
		if err != nil {
			return Die[string](err)
		}
		return Succeed(string(data))
	}
}

// GoResult retains the native value even when Go returned an error.
type GoResult[A any] struct {
	Value A
	Err   error
}
type GoError struct {
	Err     error
	Partial any
}

// FromGo defers a context-aware Go call and preserves both native return values.
// The adapter is responsible for honestly documenting whether the call observes ctx.
func FromGo[A any](call func(context.Context) (A, error)) Effect[GoResult[A]] {
	return func(fc *FiberContext) Exit[GoResult[A]] {
		value, err := call(fc.Context())
		return Succeed(GoResult[A]{Value: value, Err: err})
	}
}

func OrFail[A any](program Effect[GoResult[A]]) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		out := Invoke(fc, program)
		if out.IsFailure() {
			return Propagate[A](out)
		}
		if out.Value.Err != nil {
			return Fail[A]("GoError", GoError{out.Value.Err, out.Value.Value})
		}
		return Succeed(out.Value.Value)
	}
}
