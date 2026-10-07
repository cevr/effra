// Package hosttypes is an ordinary Go package, independent of Effra, whose
// exported declarations exercise native host types, nullable results and
// complete return tuples.
package hosttypes

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Point is a nominal value type; it is never nil.
type Point struct{ X, Y int64 }

// Origin returns a present Point.
func Origin() Point { return Point{} }

// Offset returns a moved copy of p.
func Offset(p Point, dx int64) Point { return Point{p.X + dx, p.Y} }

// DescribePoint renders p natively.
func DescribePoint(p Point) string {
	return strconv.FormatInt(p.X, 10) + "," + strconv.FormatInt(p.Y, 10)
}

// Counter is returned by pointer; a missing counter is nil.
type Counter struct{ count int }

// Find returns nil when name is unknown.
func Find(name string) *Counter {
	if name == "known" {
		return &Counter{count: 3}
	}
	return nil
}

// Count requires a present counter.
func Count(c *Counter) int { return c.count }

// Shape is a native interface. A nil Shape differs from a Shape holding a nil
// *Square, which is still a present interface value.
type Shape interface{ Kind() string }

// Square implements Shape with a pointer receiver that tolerates nil.
type Square struct{}

// Kind reports whether the dynamic pointer is nil.
func (s *Square) Kind() string {
	if s == nil {
		return "typed-nil square"
	}
	return "square"
}

// MakeShape returns a nil interface, a typed-nil payload or a square.
func MakeShape(kind string) Shape {
	switch kind {
	case "typed-nil":
		var square *Square
		return square
	case "square":
		return &Square{}
	}
	return nil
}

// ShapeKind dispatches on the native interface value.
func ShapeKind(s Shape) string { return s.Kind() }

// Problem is an error whose pointer receiver tolerates nil.
type Problem struct{ Code string }

func (p *Problem) Error() string {
	if p == nil {
		return "nil problem"
	}
	return p.Code
}

// ErrMissing is a sentinel matched with errors.Is.
var ErrMissing = errors.New("missing")

// Typed returns a partial count with a typed-nil *Problem error: Go reports
// err != nil although the dynamic pointer is nil.
func Typed() (int, error) {
	var problem *Problem
	return 7, problem
}

// Untyped returns a nil error.
func Untyped() (int, error) { return 8, nil }

// Missing wraps the sentinel and keeps a partial count.
func Missing() (int, error) { return 2, errorWrap{ErrMissing} }

type errorWrap struct{ inner error }

func (w errorWrap) Error() string { return "wrapped: " + w.inner.Error() }
func (w errorWrap) Unwrap() error { return w.inner }

// DescribeError inspects the original native error value.
func DescribeError(err error) string {
	problem, isProblem := err.(*Problem)
	switch {
	case err == nil:
		return "nil"
	case isProblem && problem == nil:
		return "typed-nil *Problem"
	case errors.Is(err, ErrMissing):
		return "is missing"
	}
	return "other: " + err.Error()
}

// ErrNoSeparator is the sentinel Split returns with its partial values.
var ErrNoSeparator = errors.New("no separator")

// Split returns three values and an error; the values are retained on error.
func Split(text string) (string, int, bool, error) {
	head, tail, found := strings.Cut(text, ":")
	if !found {
		return head, len(head), false, ErrNoSeparator
	}
	return head, len(tail), true, nil
}

// Raw is an alias of the native byte slice.
type Raw = []byte

// Bytes distinguishes a nil byte slice from a present empty one.
func Bytes(present bool) []byte {
	if !present {
		return nil
	}
	return []byte{}
}

// RawText returns its text as a byte slice through the alias.
func RawText(text string) Raw { return Raw(text) }

// BytesClass reports what Go received: nil, empty or the text.
func BytesClass(value []byte) string {
	switch {
	case value == nil:
		return "nil"
	case len(value) == 0:
		return "empty"
	}
	return string(value)
}

// Names distinguishes a nil slice from a present empty one.
func Names(present bool) []string {
	if !present {
		return nil
	}
	return []string{}
}

// NameCount reports the native length.
func NameCount(names []string) int { return len(names) }

// NewBuffer returns a type declared by another package.
func NewBuffer(text string) *bytes.Buffer { return bytes.NewBufferString(text) }

// BufferText reads the native buffer.
func BufferText(buffer *bytes.Buffer) string { return buffer.String() }

// Counts returns a native map.
func Counts() map[string]int { return map[string]int{"a": 1, "b": 2} }

// CountOf reads one key.
func CountOf(counts map[string]int, key string) int { return counts[key] }

// Boxed returns a string inside a native empty interface.
func Boxed() any { return "text" }

// DynamicType reports a value's native dynamic type.
func DynamicType(value any) string { return fmt.Sprintf("%T", value) }

// Join is variadic.
func Join(parts ...string) string { return strings.Join(parts, "") }

// Narrow uses a fixed width Effra does not admit.
func Narrow(value int32) int32 { return value }

// Identity is generic.
func Identity[T any](value T) T { return value }

// Ratio returns a float.
func Ratio() float64 { return 0.5 }
