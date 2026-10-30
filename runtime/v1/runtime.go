// Package v1 is the runtime support that lowered Ergo programs link. The
// lowering (internal/sketch/lower) copies this package's source into every
// emitted module, beside the generated main package, so a program needs no
// module download and its build key covers these exact bytes.
//
// It holds only what lowered code cannot express as ordinary Go: the host's
// providers (Console), the Go bodies of prelude extern declarations
// (Sleep), how main reports a typed failure, and the text of a basic value
// inside an error type's text. A defect is an ordinary Go panic: nothing
// here recovers one, so Go's own report names the panicking frame, which
// the //line directives map to the .ergo source.
//
// Prior art: Gleam's prelude.mjs and ReScript's runtime modules ship the
// small host-language support that generated code calls; Borgo emits Go
// that links a small runtime package.
package v1

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Console is the host's Console provider, which main binds: Println writes
// one line to standard output.
type Console struct{}

// Println writes line and a line feed to standard output in one write, so
// lines from concurrent writers do not interleave within a line.
func (Console) Println(line string) {
	_, _ = os.Stdout.WriteString(line + "\n")
}

// Sleep is the Go body of the prelude's `extern go
// ergo.local/prototype/runtime/rt.Sleep func Sleep(s LiveScheduler, d
// duration)`: it suspends the calling goroutine for d. Cancellation of a
// sleeping task is #199's; a signal to the process ends it, as Go's default
// signal handling does.
func Sleep(d time.Duration) {
	time.Sleep(d)
}

// Fail reports main's typed failure on standard error and exits with
// status 1 (lower-v1 D3: a remaining typed failure is printed, nonzero).
func Fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// The text of an error type's value comes from its fields (types.md,
// Methods 6). The lowering generates, for each type an error's text reaches,
// a function that writes the value field by field; these functions write
// the basic values in it. They take Go's basic types, so no method of a
// user type (a 196m String or Error) can change the text.

// TextBool is a bool's text: true or false.
func TextBool(b bool) string { return strconv.FormatBool(b) }

// TextInt is a signed integer's text, in decimal.
func TextInt(i int64) string { return strconv.FormatInt(i, 10) }

// TextUint is an unsigned integer's text, in decimal.
func TextUint(u uint64) string { return strconv.FormatUint(u, 10) }

// TextFloat is a float's text in the shortest form that reads back as the
// same value of its width, bits (32 or 64), as Go's %v writes it.
func TextFloat(f float64, bits int) string { return strconv.FormatFloat(f, 'g', -1, bits) }

// TextDuration is a duration's text, as Go writes a time.Duration: 1.5s.
func TextDuration(d time.Duration) string { return d.String() }

// TextList is a list's text from its elements' texts: [a b c].
func TextList(elems []string) string { return "[" + strings.Join(elems, " ") + "]" }
