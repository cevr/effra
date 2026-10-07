// Package hosttypes is an ordinary Go package, independent of Effra, whose
// exported declarations exercise native host types, nullable results and
// complete return tuples.
package hosttypes

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

// Sum has a value receiver: both Point and *Point method sets contain it.
func (p Point) Sum() int64 { return p.X + p.Y }

// Grow has a pointer receiver: a Point value is not addressable from Effra.
func (p *Point) Grow() { p.X++ }

// Increment has a pointer receiver: only *Counter can call it, and it
// mutates the counter it is called on.
func (c *Counter) Increment() int {
	c.count++
	return c.count
}

// Source reads text and also implements io.WriterTo, counting which path
// io.Copy used.
type Source struct {
	reader          *bytes.Reader
	reads, writesTo int
}

// NewSource returns a counting source over text.
func NewSource(text string) *Source { return &Source{reader: bytes.NewReader([]byte(text))} }

func (s *Source) Read(p []byte) (int, error) {
	s.reads++
	return s.reader.Read(p)
}

// WriteTo is the fast path io.Copy checks first.
func (s *Source) WriteTo(w io.Writer) (int64, error) {
	s.writesTo++
	return s.reader.WriteTo(w)
}

// Report lists how the source was used.
func (s *Source) Report() string { return fmt.Sprintf("read=%d writeTo=%d", s.reads, s.writesTo) }

// Plain is a reader with no optional methods.
type Plain struct{ reader io.Reader }

// NewPlain returns a plain reader over text.
func NewPlain(text string) *Plain { return &Plain{reader: bytes.NewReader([]byte(text))} }

func (p *Plain) Read(b []byte) (int, error) { return p.reader.Read(b) }

// Sink buffers writes and also implements io.ReaderFrom.
type Sink struct {
	buffer            bytes.Buffer
	writes, readsFrom int
}

// NewSink returns an empty counting sink.
func NewSink() *Sink { return &Sink{} }

func (s *Sink) Write(p []byte) (int, error) {
	s.writes++
	return s.buffer.Write(p)
}

// ReadFrom is the fast path io.Copy checks when the source has no WriteTo.
func (s *Sink) ReadFrom(r io.Reader) (int64, error) {
	s.readsFrom++
	return s.buffer.ReadFrom(r)
}

// Report lists how the sink was used and what it holds.
func (s *Sink) Report() string {
	return fmt.Sprintf("write=%d readFrom=%d %q", s.writes, s.readsFrom, s.buffer.String())
}

// Join is variadic.
func Join(parts ...string) string { return strings.Join(parts, "") }

// Narrow uses a fixed width Effra does not admit.
func Narrow(value int32) int32 { return value }

// Identity is generic.
func Identity[T any](value T) T { return value }

// Ratio returns a float.
func Ratio() float64 { return 0.5 }
