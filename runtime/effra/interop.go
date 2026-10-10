package effra

import "context"

// GoResult retains the native value even when Go returned an error.
type GoResult[A any] struct {
	Value A
	Err   error
}
type GoError struct {
	Err     error
	Partial any
}

// Error reports the native error, which is the failure's diagnostic message.
func (e GoError) Error() string { return e.Err.Error() }

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
